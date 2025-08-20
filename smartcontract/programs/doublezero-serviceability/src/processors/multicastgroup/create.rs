use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get_next, globalstate_write},
    helper::*,
    pda::get_multicastgroup_pda,
    state::{accounttype::AccountType, multicastgroup::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct MulticastGroupCreateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub code: String,
    pub max_bandwidth: u64,
    pub owner: Pubkey,
}

impl fmt::Debug for MulticastGroupCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, max_bandwidth: {}",
            self.code, self.max_bandwidth
        )
    }
}

pub fn process_create_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_multicastgroup({:?})", value);

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(mgroup_account.is_writable, "PDA Account is not writable");
    // get the PDA pubkey and bump seed for the account multicastgroup & check if it matches the account
    let (expected_pda_account, bump_seed) = get_multicastgroup_pda(program_id, value.index);
    assert_eq!(
        mgroup_account.key, &expected_pda_account,
        "Invalid MulticastGroup Pubkey"
    );
    assert_eq!(
        bump_seed, value.bump_seed,
        "Invalid MulticastGroup Bump Seed"
    );
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Check if the account is already initialized
    if !mgroup_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let multicastgroup = MulticastGroup {
        account_type: AccountType::MulticastGroup,
        owner: value.owner,
        index: globalstate.account_index,
        bump_seed,
        tenant_pk: Pubkey::default(),
        code,
        multicast_ip: std::net::Ipv4Addr::UNSPECIFIED,
        max_bandwidth: value.max_bandwidth,
        pub_allowlist: vec![],
        sub_allowlist: vec![],
        subscribers: vec![],
        publishers: vec![],
        status: MulticastGroupStatus::Pending,
    };

    account_create(
        mgroup_account,
        &multicastgroup,
        payer_account,
        system_program,
        program_id,
    )?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
