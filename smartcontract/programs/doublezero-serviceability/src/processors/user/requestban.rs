use crate::{
    error::DoubleZeroError,
    processors::validation::validate_program_account,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, user::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::types::NetworkV4;
use std::net::Ipv4Addr;

use super::resource_onchain_helpers;

#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct UserRequestBanArgs {
    /// Number of DzPrefixBlock accounts passed for onchain deallocation.
    /// When 0, legacy behavior (PendingBan status). When > 0, atomic deallocation + Banned.
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
    /// Whether MulticastPublisherBlock account is passed (1 = yes, 0 = no).
    #[incremental(default = 0)]
    pub multicast_publisher_count: u8,
}

impl fmt::Debug for UserRequestBanArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "dz_prefix_count: {}, multicast_publisher_count: {}",
            self.dz_prefix_count, self.multicast_publisher_count
        )
    }
}

pub fn process_request_ban_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserRequestBanArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Account layout WITH deallocation (dz_prefix_count > 0):
    //   [user, globalstate, user_tunnel_block, multicast_publisher_block?, device_tunnel_ids, dz_prefix_0..N, payer, system]
    // Account layout WITHOUT (legacy, dz_prefix_count == 0):
    //   [user, globalstate, payer, system]
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

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_request_ban_user({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(
        user_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "User"
    );
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

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::try_from(user_account)?;
    if !can_request_ban(user.status) {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    if let Some((
        user_tunnel_block_ext,
        multicast_publisher_block_ext,
        device_tunnel_ids_ext,
        dz_prefix_accounts,
    )) = deallocation_accounts
    {
        // Atomic path: deallocate resources and set status to Banned
        if !user.publishers.is_empty() || !user.subscribers.is_empty() {
            #[cfg(test)]
            msg!("{:?}", user);
            return Err(DoubleZeroError::ReferenceCountNotZero.into());
        }

        resource_onchain_helpers::validate_and_deallocate_user_resources(
            program_id,
            &user,
            user_tunnel_block_ext,
            multicast_publisher_block_ext.as_ref().map(|a| *a),
            device_tunnel_ids_ext,
            &dz_prefix_accounts,
            &globalstate,
        )?;

        // Zero out deallocated fields so subsequent delete sees them as already-deallocated
        user.tunnel_net = NetworkV4::default();
        user.tunnel_id = 0;
        user.dz_ip = Ipv4Addr::UNSPECIFIED;
        user.status = UserStatus::Banned;

        #[cfg(test)]
        msg!("RequestBanUser (atomic): User resources deallocated, status = Banned");
    } else {
        // Legacy path: set status to PendingBan for activator to handle
        user.status = UserStatus::PendingBan;

        #[cfg(test)]
        msg!("RequestBanUser (legacy): status = PendingBan");
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}

fn can_request_ban(status: UserStatus) -> bool {
    status == UserStatus::Activated || status == UserStatus::SuspendedDeprecated
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn request_ban_allowed_statuses() {
        assert!(can_request_ban(UserStatus::Activated));
        assert!(can_request_ban(UserStatus::SuspendedDeprecated));
    }

    #[test]
    fn request_ban_disallowed_statuses() {
        assert!(!can_request_ban(UserStatus::Pending));
        assert!(!can_request_ban(UserStatus::Deleting));
        assert!(!can_request_ban(UserStatus::Rejected));
        assert!(!can_request_ban(UserStatus::PendingBan));
        assert!(!can_request_ban(UserStatus::Banned));
        assert!(!can_request_ban(UserStatus::Updating));
        assert!(!can_request_ban(UserStatus::OutOfCredits));
    }
}
