use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, user::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;

#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct UserRequestBanArgs {}

impl fmt::Debug for UserRequestBanArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_request_ban_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &UserRequestBanArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_banning_user({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(user_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::try_from(user_account)?;
    if !can_request_ban(user.status) {
        return Err(DoubleZeroError::InvalidStatus.into());
    }
    user.status = UserStatus::PendingBan;

    try_acc_write(&user, user_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Deleting: {:?}", user);

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
