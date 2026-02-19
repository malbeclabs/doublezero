use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{contributor::*, globalstate::GlobalState},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::validate_account_code;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct ContributorUpdateArgs {
    pub code: Option<String>,
    pub owner: Option<Pubkey>,
    pub ops_manager_pk: Option<Pubkey>,
}

impl fmt::Debug for ContributorUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, owner: {:?}, ops_manager_pk: {:?}",
            self.code, self.owner, self.ops_manager_pk
        )
    }
}

pub fn process_update_contributor(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ContributorUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_contributor({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(
        contributor_account.is_writable,
        "PDA Account is not writable"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    let mut contributor = Contributor::try_from(contributor_account)?;
    let globalstate = GlobalState::try_from(globalstate_account)?;

    let only_ops_manager_update =
        value.code.is_none() && value.owner.is_none() && value.ops_manager_pk.is_some();

    // If only ops_manager_pk is being updated, allow contributor owner or foundation allowlist
    // Otherwise, only allow foundation allowlist
    let is_authorized = if only_ops_manager_update {
        globalstate.foundation_allowlist.contains(payer_account.key)
            || contributor.owner == *payer_account.key
    } else {
        globalstate.foundation_allowlist.contains(payer_account.key)
    };

    if !is_authorized {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let Some(ref code) = value.code {
        contributor.code =
            validate_account_code(code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
    }
    if let Some(ref owner) = value.owner {
        contributor.owner = *owner;
    }
    if let Some(ref ops_manager_pk) = value.ops_manager_pk {
        contributor.ops_manager_pk = *ops_manager_pk;
    }
    try_acc_write(&contributor, contributor_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", contributor);

    Ok(())
}
