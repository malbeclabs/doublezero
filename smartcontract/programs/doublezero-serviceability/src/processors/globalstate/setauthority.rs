use crate::{
    error::DoubleZeroError, pda::*, serializer::try_acc_write, state::globalstate::GlobalState,
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
pub struct SetAuthorityArgs {
    pub activator_authority_pk: Option<Pubkey>,
    pub sentinel_authority_pk: Option<Pubkey>,
    pub health_oracle_pk: Option<Pubkey>,
}

impl fmt::Debug for SetAuthorityArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "activator_authority_pk: {:?}, sentinel_authority_pk: {:?}, health_oracle_pk: {:?}",
            self.activator_authority_pk, self.sentinel_authority_pk, self.health_oracle_pk
        )
    }
}

pub fn process_set_authority(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetAuthorityArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_authority({:?})", value);

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

    // Parse the global state account & check if the payer is in the allowlist
    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let Some(activator_authority_pk) = value.activator_authority_pk {
        globalstate.activator_authority_pk = activator_authority_pk;
    }

    if let Some(sentinel_authority_pk) = value.sentinel_authority_pk {
        globalstate.sentinel_authority_pk = sentinel_authority_pk;
    }
    if let Some(health_oracle_pk) = value.health_oracle_pk {
        globalstate.health_oracle_pk = health_oracle_pk;
    }

    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", globalstate);

    Ok(())
}
