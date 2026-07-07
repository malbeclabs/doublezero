use crate::{
    authorize::authorize,
    pda::*,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, permission::permission_flags},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct AddQaAllowlistArgs {
    pub pubkey: Pubkey,
}

impl fmt::Debug for AddQaAllowlistArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "pubkey: {}", self.pubkey)
    }
}

pub fn process_add_qa_allowlist_globalconfig(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &AddQaAllowlistArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_add_qa_allowlist_globalconfig({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(
        globalstate_account.is_writable,
        "PDA Account is not writable"
    );

    let (expected_pda_account, _) = get_globalstate_pda(program_id);
    assert_eq!(
        globalstate_account.key, &expected_pda_account,
        "Invalid GlobalState PubKey"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    // Authorization: GLOBALSTATE_ADMIN (Permission account) or foundation (legacy).
    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::GLOBALSTATE_ADMIN,
    )?;

    if globalstate.qa_allowlist.contains(&value.pubkey) {
        return Err(ProgramError::InvalidArgument);
    }
    globalstate.qa_allowlist.push(value.pubkey);

    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", globalstate);

    Ok(())
}
