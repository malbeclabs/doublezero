use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get, globalstate_write_with_realloc},
    pda::get_globalstate_pda,
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

#[derive(BorshSerialize, BorshDeserializeIncremental, Clone, PartialEq)]
pub struct SetAirdropArgs {
    pub contributor_airdrop_lamports: Option<u64>,
    pub user_airdrop_lamports: Option<u64>,
}

impl fmt::Debug for SetAirdropArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "contributor_airdrop_lamports: {:?}, user_airdrop_lamports: {:?}",
            self.contributor_airdrop_lamports, self.user_airdrop_lamports,
        )
    }
}

pub fn process_set_airdrop(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetAirdropArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_airdrop({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid PDA Account Owner",
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );

    let (expected_pda_account, bump_seed) = get_globalstate_pda(program_id);
    assert_eq!(
        globalstate_account.key, &expected_pda_account,
        "Invalid GlobalState Pubkey",
    );

    // Fetch the globalstate and ensure payer authorization to adjust airdrop
    let mut globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let Some(contributor_airdrop_lamports) = value.contributor_airdrop_lamports {
        globalstate.contributor_airdrop_lamports = contributor_airdrop_lamports;
    }

    if let Some(user_airdrop_lamports) = value.user_airdrop_lamports {
        globalstate.user_airdrop_lamports = user_airdrop_lamports;
    }

    globalstate_write_with_realloc(
        globalstate_account,
        &globalstate,
        payer_account,
        system_program,
        bump_seed,
    );

    #[cfg(test)]
    msg!("Updated: {:?}", globalstate);

    Ok(())
}
