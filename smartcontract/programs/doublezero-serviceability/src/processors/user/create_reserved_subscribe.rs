use crate::{
    error::DoubleZeroError,
    pda::get_user_pda,
    processors::validation::validate_program_account,
    seeds::{SEED_PREFIX, SEED_USER},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType,
        device::{Device, DeviceStatus},
        globalstate::GlobalState,
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
        reservation::Reservation,
        tenant::Tenant,
        user::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

use super::resource_onchain_helpers;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct CreateReservedSubscribeUserArgs {
    pub user_type: UserType,
    pub cyoa_type: UserCYOA,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub tunnel_endpoint: Ipv4Addr,
    pub publisher: bool,
    pub subscriber: bool,
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
}

impl fmt::Debug for CreateReservedSubscribeUserArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, client_ip: {}, tunnel_endpoint: {}, publisher: {}, subscriber: {}, dz_prefix_count: {}",
            self.user_type,
            self.cyoa_type,
            &self.client_ip,
            &self.tunnel_endpoint,
            self.publisher,
            self.subscriber,
            self.dz_prefix_count,
        )
    }
}

/// Create a multicast subscriber user using a reservation to bypass the normal capacity check,
/// and subscribe them to a multicast group.
///
/// Account layout:
///   [user, device, mgroup, reservation, tenant, globalstate, [resource_ext...], payer, system]
///
/// No access pass is required — the payer must be the reservation authority or on the foundation allowlist.
/// Multicast group allowlist checks are skipped since the payer is already authorized.
pub fn process_create_reserved_subscribe_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &CreateReservedSubscribeUserArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let mgroup_account = next_account_info(accounts_iter)?;
    let reservation_account = next_account_info(accounts_iter)?;
    let tenant_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension accounts for on-chain allocation
    let resource_extension_accounts = if value.dz_prefix_count > 0 {
        let user_tunnel_block_ext = next_account_info(accounts_iter)?;
        let multicast_publisher_block_ext = next_account_info(accounts_iter)?;
        let device_tunnel_ids_ext = next_account_info(accounts_iter)?;

        let mut dz_prefix_accounts = Vec::with_capacity(value.dz_prefix_count as usize);
        for _ in 0..value.dz_prefix_count {
            dz_prefix_accounts.push(next_account_info(accounts_iter)?);
        }

        Some((
            user_tunnel_block_ext,
            multicast_publisher_block_ext,
            device_tunnel_ids_ext,
            dz_prefix_accounts,
        ))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    msg!("process_create_reserved_subscribe_user({:?})", value);

    // Validate payer is signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate user account is empty (not yet created)
    if !user_account.data_is_empty() {
        return Err(solana_program::program_error::ProgramError::AccountAlreadyInitialized);
    }

    // Validate device
    validate_program_account!(
        device_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "Device"
    );
    if device_account.data.borrow()[0] != AccountType::Device as u8 {
        return Err(DoubleZeroError::InvalidDevicePubkey.into());
    }

    // Validate multicast group
    validate_program_account!(
        mgroup_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "MulticastGroup"
    );

    // Validate reservation
    validate_program_account!(
        reservation_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "Reservation"
    );

    // Validate tenant
    validate_program_account!(
        tenant_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "Tenant"
    );
    if tenant_account.data.borrow()[0] != AccountType::Tenant as u8 {
        return Err(DoubleZeroError::InvalidAccountType.into());
    }

    // Validate globalstate
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "GlobalState"
    );

    // Load globalstate to check authorization
    let globalstate = GlobalState::try_from(globalstate_account)?;
    let is_qa = globalstate.qa_allowlist.contains(payer_account.key);
    if globalstate.reservation_authority_pk != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Load and validate device
    let mut device = Device::try_from(device_account)?;

    // Only activated devices can have users (or foundation/QA allowlist)
    if device.status != DeviceStatus::Activated
        && !globalstate.foundation_allowlist.contains(payer_account.key)
        && !is_qa
    {
        msg!("{:?}", device);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // Validate and consume reservation
    let mut reservation = Reservation::try_from(reservation_account)?;
    if reservation.device_pk != *device_account.key {
        msg!(
            "Reservation device_pk {} does not match device {}",
            reservation.device_pk,
            device_account.key
        );
        return Err(DoubleZeroError::InvalidDevicePubkey.into());
    }
    if reservation.owner != *payer_account.key {
        msg!(
            "Reservation owner {} does not match payer {}",
            reservation.owner,
            payer_account.key
        );
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if reservation.reserved_count == 0 {
        msg!("Reservation has no remaining seats");
        return Err(DoubleZeroError::MaxUsersExceeded.into());
    }

    // Consume one seat from the reservation
    reservation.reserved_count -= 1;
    device.reserved_seats = device
        .reserved_seats
        .checked_sub(1)
        .ok_or(DoubleZeroError::InvalidArgument)?;

    try_acc_write(&reservation, reservation_account, payer_account, accounts)?;

    // This instruction is for multicast subscribers only
    if value.user_type != UserType::Multicast {
        msg!(
            "CreateReservedSubscribeUser requires Multicast user type, got: {}",
            value.user_type
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    // Check multicast limit (QA bypass skips this)
    if !is_qa
        && device.max_multicast_users > 0
        && device.multicast_users_count >= device.max_multicast_users
    {
        msg!(
            "Max multicast users exceeded: count={}, max={}",
            device.multicast_users_count,
            device.max_multicast_users
        );
        return Err(DoubleZeroError::MaxMulticastUsersExceeded.into());
    }

    // Update device counters
    device.reference_count += 1;
    device.users_count += 1;
    device.multicast_users_count += 1;

    // Update tenant reference count
    let mut tenant = Tenant::try_from(tenant_account)?;
    tenant.reference_count = tenant
        .reference_count
        .checked_add(1)
        .ok_or(DoubleZeroError::InvalidIndex)?;
    try_acc_write(&tenant, tenant_account, payer_account, accounts)?;

    // Validate user PDA
    let (expected_pda, bump_seed) = get_user_pda(program_id, &value.client_ip, value.user_type);
    assert_eq!(user_account.key, &expected_pda, "Invalid User PDA");

    // Build user
    let mut user = User {
        account_type: AccountType::User,
        owner: *payer_account.key,
        bump_seed,
        index: 0,
        tenant_pk: *tenant_account.key,
        user_type: value.user_type,
        device_pk: *device_account.key,
        cyoa_type: value.cyoa_type,
        client_ip: value.client_ip,
        dz_ip: Ipv4Addr::UNSPECIFIED,
        tunnel_id: 0,
        tunnel_net: NetworkV4::default(),
        status: UserStatus::Pending,
        publishers: vec![],
        subscribers: vec![],
        validator_pubkey: Pubkey::default(),
        tunnel_endpoint: value.tunnel_endpoint,
    };

    // Subscribe user to multicast group (no access pass allowlist checks — payer is already authorized above)
    let mut mgroup = MulticastGroup::try_from(mgroup_account)?;
    if mgroup.status != MulticastGroupStatus::Activated {
        msg!("MulticastGroupStatus: {:?}", mgroup.status);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    if value.publisher && !user.publishers.contains(mgroup_account.key) {
        mgroup.publisher_count = mgroup.publisher_count.saturating_add(1);
        user.publishers.push(*mgroup_account.key);
    }
    if value.subscriber && !user.subscribers.contains(mgroup_account.key) {
        mgroup.subscriber_count = mgroup.subscriber_count.saturating_add(1);
        user.subscribers.push(*mgroup_account.key);
    }

    // Atomic on-chain allocation if requested
    if let Some((
        user_tunnel_block_ext,
        multicast_publisher_block_ext,
        device_tunnel_ids_ext,
        dz_prefix_accounts,
    )) = resource_extension_accounts
    {
        let globalstate_ref = GlobalState::try_from(globalstate_account)?;
        resource_onchain_helpers::validate_and_allocate_user_resources(
            program_id,
            &mut user,
            user_tunnel_block_ext,
            multicast_publisher_block_ext,
            device_tunnel_ids_ext,
            &dz_prefix_accounts,
            &globalstate_ref,
        )?;

        // Activate without access pass — just set status directly
        user.status = UserStatus::Activated;
    }

    // Create user account
    try_acc_create(
        &user,
        user_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_USER,
            &user.client_ip.octets(),
            &[user.user_type as u8],
            &[bump_seed],
        ],
    )?;

    // Write device and multicast group
    try_acc_write(&device, device_account, payer_account, accounts)?;
    try_acc_write(&mgroup, mgroup_account, payer_account, accounts)?;

    Ok(())
}
