use crate::{
    error::DoubleZeroError,
    pda::{get_accesspass_pda, get_user_old_pda, get_user_pda},
    seeds::{SEED_PREFIX, SEED_USER},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::AccountType,
        device::{Device, DeviceStatus},
        globalstate::GlobalState,
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
    clock::Clock,
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
    sysvar::Sysvar,
};
use std::net::Ipv4Addr;

use super::resource_onchain_helpers;
use crate::processors::validation::validate_program_account;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct UserCreateArgs {
    pub user_type: UserType,
    pub cyoa_type: UserCYOA,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: std::net::Ipv4Addr,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub tunnel_endpoint: std::net::Ipv4Addr,
    /// Number of DzPrefixBlock accounts passed for on-chain allocation.
    /// When 0, legacy behavior is used (Pending status). When > 0, atomic create+allocate+activate.
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
}

impl fmt::Debug for UserCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, client_ip: {}, tunnel_endpoint: {}, dz_prefix_count: {}",
            self.user_type,
            self.cyoa_type,
            &self.client_ip,
            &self.tunnel_endpoint,
            self.dz_prefix_count,
        )
    }
}

#[derive(PartialEq)]
enum PDAVersion {
    V1,
    V2,
}

pub fn process_create_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension accounts for on-chain allocation (between globalstate and optional_tenant)
    // Account layout WITH ResourceExtension (dz_prefix_count > 0):
    //   [user, device, accesspass, globalstate, user_tunnel_block, multicast_publisher_block, device_tunnel_ids, dz_prefix_0..N, optional_tenant, payer, system]
    // Account layout WITHOUT (legacy, dz_prefix_count == 0):
    //   [user, device, accesspass, globalstate, optional_tenant, payer, system]
    let resource_extension_accounts = if value.dz_prefix_count > 0 {
        let user_tunnel_block_ext = next_account_info(accounts_iter)?; // UserTunnelBlock
        let multicast_publisher_block_ext = next_account_info(accounts_iter)?; // MulticastPublisherBlock
        let device_tunnel_ids_ext = next_account_info(accounts_iter)?; // TunnelIds

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

    // Parse optional tenant account
    // With resource extensions, the total fixed accounts shift:
    //   Legacy without tenant: 6 accounts [user, device, accesspass, globalstate, payer, system]
    //   Legacy with tenant: 7 accounts [user, device, accesspass, globalstate, tenant, payer, system]
    //   Atomic without tenant: 6 + 3 + dz_prefix_count accounts
    //   Atomic with tenant: 7 + 3 + dz_prefix_count accounts
    let resource_ext_accounts = if value.dz_prefix_count > 0 {
        3 + value.dz_prefix_count as usize
    } else {
        0
    };
    let tenant_account = if accounts.len() >= 7 + resource_ext_accounts {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    msg!("process_create_user({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate tenant account if provided
    if let Some(tenant_account) = tenant_account {
        // Must not be empty (already initialized)
        if tenant_account.data_is_empty() {
            return Err(DoubleZeroError::InvalidTenantPubkey.into());
        }

        validate_program_account!(
            tenant_account,
            program_id,
            writable = true,
            pda = None::<&Pubkey>,
            "Tenant"
        );

        // Verify account type is Tenant
        if tenant_account.data.borrow()[0] != AccountType::Tenant as u8 {
            return Err(DoubleZeroError::InvalidAccountType.into());
        }
    }

    if !user_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    validate_program_account!(
        accesspass_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "AccessPass"
    );

    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    globalstate.account_index += 1;

    let (expected_old_pda_account, bump_old_seed) =
        get_user_old_pda(program_id, globalstate.account_index);
    let (expected_pda_account, bump_seed) =
        get_user_pda(program_id, &value.client_ip, value.user_type);

    let pda_ver = if user_account.key == &expected_pda_account {
        PDAVersion::V2
    } else if user_account.key == &expected_old_pda_account {
        PDAVersion::V1
    } else {
        msg!(
            "Invalid User PDA. expected: {} or {}, found: {}",
            expected_pda_account,
            expected_old_pda_account,
            user_account.key
        );
        return Err(DoubleZeroError::InvalidUserPubkey.into());
    };

    // Check account Types
    if device_account.data_is_empty()
        || device_account.data.borrow()[0] != AccountType::Device as u8
    {
        return Err(DoubleZeroError::InvalidDevicePubkey.into());
    }
    if device_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &value.client_ip, payer_account.key);
    let (accesspass_dynamic_pda, _) =
        get_accesspass_pda(program_id, &Ipv4Addr::UNSPECIFIED, payer_account.key);
    // Access Pass must exist and match the client_ip or allow_multiple_ip must be enabled
    assert!(
        accesspass_account.key == &accesspass_pda
            || accesspass_account.key == &accesspass_dynamic_pda,
        "Invalid AccessPass PDA",
    );

    // Read Access Pass
    let mut accesspass = AccessPass::try_from(accesspass_account)?;
    if accesspass.user_payer != *payer_account.key {
        msg!(
            "Invalid user_payer accesspass.{{user_payer: {}}} = {{ user_payer: {} }}",
            accesspass.user_payer,
            payer_account.key
        );
        return Err(DoubleZeroError::Unauthorized.into());
    }
    if accesspass.is_dynamic() && accesspass.client_ip == Ipv4Addr::UNSPECIFIED {
        accesspass.client_ip = value.client_ip; // lock to the first used IP
    }
    if !accesspass.allow_multiple_ip() && accesspass.client_ip != value.client_ip {
        msg!(
            "Invalid client_ip accesspass.{{client_ip: {}}} = {{ client_ip: {} }}",
            accesspass.client_ip,
            value.client_ip
        );
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Enforce tenant_allowlist: if access-pass has a non-default tenant in its
    // allowlist, the user's tenant must be in that list.
    if accesspass
        .tenant_allowlist
        .iter()
        .any(|pk| *pk != Pubkey::default())
    {
        let user_tenant_pk = tenant_account.map(|a| *a.key).unwrap_or(Pubkey::default());
        if !accesspass.tenant_allowlist.contains(&user_tenant_pk) {
            msg!(
                "Tenant {} not in access-pass tenant_allowlist {:?}",
                user_tenant_pk,
                accesspass.tenant_allowlist
            );
            return Err(DoubleZeroError::TenantNotInAccessPassAllowlist.into());
        }
    } else if let Some(tenant_account) = tenant_account {
        let tenant = Tenant::try_from(tenant_account)?;
        msg!(
            "Access-pass has no tenant_allowlist, but user creation specifies tenant {}",
            tenant.code
        );
        return Err(DoubleZeroError::TenantNotInAccessPassAllowlist.into());
    }

    // Check Initial epoch
    let clock = Clock::get()?;
    let current_epoch = clock.epoch;
    if accesspass.last_access_epoch < current_epoch {
        msg!(
            "Invalid epoch last_access_epoch: {} < current_epoch: {}",
            accesspass.last_access_epoch,
            current_epoch
        );
        return Err(DoubleZeroError::AccessPassUnauthorized.into());
    }

    // Read validator_pubkey from AccessPass
    let validator_pubkey = match &accesspass.accesspass_type {
        AccessPassType::SolanaValidator(pk) => *pk,
        _ => Pubkey::default(),
    };

    let mut device = Device::try_from(device_account)?;

    let is_qa = globalstate.qa_allowlist.contains(payer_account.key);

    // Only activated devices can have users, or if in foundation allowlist
    if device.status != DeviceStatus::Activated
        && !globalstate.foundation_allowlist.contains(payer_account.key)
        && !is_qa
    {
        msg!("{:?}", device);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    if device.users_count >= device.max_users && !is_qa {
        msg!("{:?}", device);
        return Err(DoubleZeroError::MaxUsersExceeded.into());
    }

    // Check per-type limits (when max > 0, the limit is enforced)
    match value.user_type {
        UserType::Multicast => {
            if device.max_multicast_users > 0
                && device.multicast_users_count >= device.max_multicast_users
                && !is_qa
            {
                msg!(
                    "Max multicast users exceeded: count={}, max={}",
                    device.multicast_users_count,
                    device.max_multicast_users
                );
                return Err(DoubleZeroError::MaxMulticastUsersExceeded.into());
            }
        }
        _ => {
            if device.max_unicast_users > 0
                && device.unicast_users_count >= device.max_unicast_users
                && !is_qa
            {
                msg!(
                    "Max unicast users exceeded: count={}, max={}",
                    device.unicast_users_count,
                    device.max_unicast_users
                );
                return Err(DoubleZeroError::MaxUnicastUsersExceeded.into());
            }
        }
    }

    // All validations passed - now update counters
    accesspass.connection_count += 1;
    accesspass.status = AccessPassStatus::Connected;

    device.reference_count += 1;
    device.users_count += 1;
    // Increment per-type counter
    match value.user_type {
        UserType::Multicast => {
            device.multicast_users_count += 1;
        }
        _ => {
            device.unicast_users_count += 1;
        }
    }

    // Handle tenant reference counting and get tenant_pk
    let tenant_pk = if let Some(tenant_account) = tenant_account {
        let mut tenant = Tenant::try_from(tenant_account)?;

        tenant.reference_count = tenant
            .reference_count
            .checked_add(1)
            .ok_or(DoubleZeroError::InvalidIndex)?;

        try_acc_write(&tenant, tenant_account, payer_account, accounts)?;

        *tenant_account.key
    } else {
        Pubkey::default()
    };

    let mut user: User = User {
        account_type: AccountType::User,
        owner: *payer_account.key,
        bump_seed: if pda_ver == PDAVersion::V1 {
            bump_old_seed
        } else {
            bump_seed
        },
        index: if pda_ver == PDAVersion::V1 {
            globalstate.account_index
        } else {
            0
        },
        tenant_pk,
        user_type: value.user_type,
        device_pk: *device_account.key,
        cyoa_type: value.cyoa_type,
        client_ip: value.client_ip,
        dz_ip: std::net::Ipv4Addr::UNSPECIFIED,
        tunnel_id: 0,
        tunnel_net: NetworkV4::default(),
        status: UserStatus::Pending,
        publishers: vec![],
        subscribers: vec![],
        validator_pubkey,
        tunnel_endpoint: value.tunnel_endpoint,
    };

    // Atomic create+allocate+activate if on-chain allocation is requested
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

        user.try_activate(&mut accesspass)?;
    }

    if pda_ver == PDAVersion::V1 {
        try_acc_create(
            &user,
            user_account,
            payer_account,
            system_program,
            program_id,
            &[
                SEED_PREFIX,
                SEED_USER,
                &user.index.to_le_bytes(),
                &[bump_old_seed],
            ],
        )?;
        try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;
    } else {
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
        )?
    }

    try_acc_write(&device, device_account, payer_account, accounts)?;
    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

    Ok(())
}
