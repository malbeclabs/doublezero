use crate::{
    error::DoubleZeroError,
    processors::validation::validate_program_account,
    serializer::{try_acc_close, try_acc_write},
    state::{device::Device, globalstate::GlobalState, multicastgroup::MulticastGroup, user::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

use super::resource_onchain_helpers;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeleteReservedSubscribeUserArgs {
    /// Number of DzPrefixBlock accounts passed for on-chain deallocation.
    /// When 0, no resource deallocation. When > 0, atomic unsubscribe+deallocate+close.
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
    /// Whether MulticastPublisherBlock account is passed (1 = yes, 0 = no).
    #[incremental(default = 0)]
    pub multicast_publisher_count: u8,
}

impl fmt::Debug for DeleteReservedSubscribeUserArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "dz_prefix_count: {}, multicast_publisher_count: {}",
            self.dz_prefix_count, self.multicast_publisher_count
        )
    }
}

/// Delete a user that was created via CreateReservedSubscribeUser.
///
/// This instruction unsubscribes the user from all multicast groups, decrements
/// device counters, and closes the user account — all without requiring an access pass.
///
/// Account layout:
///   [user, device, mgroup, globalstate, [resource_ext...], owner, payer, system]
///
/// No access pass is required — the payer must be the reservation authority or on the foundation allowlist.
pub fn process_delete_reserved_subscribe_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeleteReservedSubscribeUserArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let mgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension accounts for on-chain deallocation
    let deallocation_accounts = if value.dz_prefix_count > 0 {
        let user_tunnel_block_ext = next_account_info(accounts_iter)?;

        let multicast_publisher_block_ext = if value.multicast_publisher_count > 0 {
            Some(next_account_info(accounts_iter)?)
        } else {
            None
        };

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

    let owner_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    msg!("process_delete_reserved_subscribe_user({:?})", value);

    // Validate payer is signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate user account
    validate_program_account!(
        user_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "User"
    );

    // Validate device account
    validate_program_account!(
        device_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "Device"
    );

    // Validate multicast group account
    validate_program_account!(
        mgroup_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "MulticastGroup"
    );

    // Validate globalstate
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "GlobalState"
    );

    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    // Load globalstate to check authorization
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if globalstate.reservation_authority_pk != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Load user
    let user = User::try_from(user_account)?;

    // Verify user belongs to this device
    if user.device_pk != *device_account.key {
        msg!(
            "User device_pk {} does not match device {}",
            user.device_pk,
            device_account.key
        );
        return Err(DoubleZeroError::InvalidDevicePubkey.into());
    }

    // Verify owner account matches user owner
    if user.owner != *owner_account.key {
        msg!(
            "Owner account {} does not match user owner {}",
            owner_account.key,
            user.owner
        );
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Unsubscribe from multicast group
    let mut mgroup = MulticastGroup::try_from(mgroup_account)?;
    if user.publishers.contains(mgroup_account.key) {
        mgroup.publisher_count = mgroup.publisher_count.saturating_sub(1);
    }
    if user.subscribers.contains(mgroup_account.key) {
        mgroup.subscriber_count = mgroup.subscriber_count.saturating_sub(1);
    }
    try_acc_write(&mgroup, mgroup_account, payer_account, accounts)?;

    // Deallocate resources if requested
    if let Some((
        user_tunnel_block_ext,
        multicast_publisher_block_ext,
        device_tunnel_ids_ext,
        dz_prefix_accounts,
    )) = deallocation_accounts
    {
        resource_onchain_helpers::validate_and_deallocate_user_resources(
            program_id,
            &user,
            user_tunnel_block_ext,
            multicast_publisher_block_ext.as_ref().map(|a| *a),
            device_tunnel_ids_ext,
            &dz_prefix_accounts,
            &globalstate,
        )?;
    }

    // Decrement device counters
    let mut device = Device::try_from(device_account)?;
    device.reference_count = device.reference_count.saturating_sub(1);
    device.users_count = device.users_count.saturating_sub(1);
    match user.user_type {
        UserType::Multicast => {
            if !user.publishers.is_empty() {
                device.multicast_publishers_count =
                    device.multicast_publishers_count.saturating_sub(1);
            } else {
                device.multicast_subscribers_count =
                    device.multicast_subscribers_count.saturating_sub(1);
            }
        }
        _ => {
            device.unicast_users_count = device.unicast_users_count.saturating_sub(1);
        }
    }
    try_acc_write(&device, device_account, payer_account, accounts)?;

    // Close user account, return rent to owner
    try_acc_close(user_account, owner_account)?;

    Ok(())
}
