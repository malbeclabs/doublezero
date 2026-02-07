use crate::{
    error::DoubleZeroError,
    pda::*,
    seeds::{SEED_PREFIX, SEED_TENANT},
    serializer::try_acc_create,
    state::{accounttype::AccountType, globalstate::GlobalState, tenant::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program::invoke_signed_unchecked,
    program_error::ProgramError,
    pubkey::Pubkey,
    rent::Rent,
    system_instruction,
    sysvar::Sysvar,
};
use std::fmt;

#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct TenantCreateArgs {
    pub code: String,
    pub vrf_id: u16,
}

impl fmt::Debug for TenantCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "code: {}, vrf_id: {}", self.code, self.vrf_id)
    }
}

pub fn process_create_tenant(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TenantCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let tenant_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_tenant({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

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
    assert!(tenant_account.is_writable, "PDA Account is not writable");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;

    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }
    // get the PDA pubkey and bump seed for the account tenant & check if it matches the account
    let (expected_pda_account, bump_seed) = get_tenant_pda(program_id, &code);
    assert_eq!(
        tenant_account.key, &expected_pda_account,
        "Invalid Tenant PubKey"
    );

    // Check if the account is already initialized
    if !tenant_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let tenant = Tenant {
        account_type: AccountType::Tenant,
        owner: *owner_account.key,
        reference_count: 0,
        bump_seed,
        code: code.clone(),
        vrf_id: value.vrf_id,
        administrators: vec![],
    };

    let deposit = Rent::get()
        .unwrap()
        .minimum_balance(0)
        .saturating_add(globalstate.contributor_airdrop_lamports);

    invoke_signed_unchecked(
        &system_instruction::transfer(payer_account.key, owner_account.key, deposit),
        &[
            payer_account.clone(),
            owner_account.clone(),
            system_program.clone(),
        ],
        &[],
    )?;

    try_acc_create(
        &tenant,
        tenant_account,
        payer_account,
        system_program,
        program_id,
        &[SEED_PREFIX, SEED_TENANT, code.as_bytes(), &[bump_seed]],
    )?;

    Ok(())
}
