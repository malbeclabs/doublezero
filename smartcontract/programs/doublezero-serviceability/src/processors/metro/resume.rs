use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{
        globalstate::GlobalState,
        metro::{Metro, MetroStatus},
    },
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
pub struct MetroResumeArgs {}

impl fmt::Debug for MetroResumeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_resume_metro(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &MetroResumeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let metro_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_resume_metro({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(metro_account.owner, program_id, "Invalid PDA Account Owner");
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
    assert!(metro_account.is_writable, "PDA Account is not writable");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    // Parse accounts
    let globalstate = GlobalState::try_from(globalstate_account)?;
    let mut metro: Metro = Metro::try_from(metro_account)?;

    // Authorization:
    //  - Only accounts in the foundation_allowlist may resume the metro.
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Only resume metros that are currently Suspended
    if metro.status != MetroStatus::Suspended {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    metro.status = MetroStatus::Activated;

    try_acc_write(&metro, metro_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Resumed: {:?}", metro);

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::globalstate::GlobalState;

    #[test]
    fn payer_not_in_foundation_allowlist_cannot_resume() {
        let payer = Pubkey::new_unique();

        let globalstate = GlobalState::default();

        let is_foundation = globalstate.foundation_allowlist.contains(&payer);
        assert!(!is_foundation);
    }

    #[test]
    fn payer_in_foundation_allowlist_can_resume() {
        let payer = Pubkey::new_unique();

        let mut globalstate = GlobalState::default();

        // Not in allowlist: should fail auth condition
        let is_foundation = globalstate.foundation_allowlist.contains(&payer);
        assert!(!is_foundation);

        // After adding to allowlist: should pass auth condition
        globalstate.foundation_allowlist.push(payer);
        let is_foundation = globalstate.foundation_allowlist.contains(&payer);
        assert!(is_foundation);
    }
}
