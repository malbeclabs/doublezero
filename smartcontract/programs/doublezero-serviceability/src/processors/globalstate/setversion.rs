use crate::{
    error::DoubleZeroError,
    pda::*,
    programversion::ProgramVersion,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, programconfig::ProgramConfig},
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
pub struct SetVersionArgs {
    pub min_compatible_version: ProgramVersion,
}

impl fmt::Debug for SetVersionArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "min_compatible_version: {:?}",
            self.min_compatible_version
        )
    }
}

pub fn process_set_version(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetVersionArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let program_config_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_version({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        program_config_account.owner, program_id,
        "Invalid ProgramVersion Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(
        globalstate_account.is_writable,
        "PDA Account is not writable"
    );

    let (expected_pda_account, _bump_seed) = get_globalstate_pda(program_id);
    assert_eq!(
        globalstate_account.key, &expected_pda_account,
        "Invalid GlobalState PubKey"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut program_config = ProgramConfig::try_from(program_config_account)?;

    if value.min_compatible_version > program_config.version {
        return Err(DoubleZeroError::InvalidMinCompatibleVersion.into());
    }

    program_config.min_compatible_version = value.min_compatible_version.clone();

    try_acc_write(
        &program_config,
        program_config_account,
        payer_account,
        accounts,
    )?;

    #[cfg(test)]
    msg!("Updated: {:?}", globalstate);

    Ok(())
}
