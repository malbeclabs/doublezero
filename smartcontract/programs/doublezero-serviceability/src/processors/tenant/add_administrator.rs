use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, tenant::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct TenantAddAdministratorArgs {
    pub administrator: Pubkey,
}

impl fmt::Debug for TenantAddAdministratorArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "administrator: {}", self.administrator)
    }
}

pub fn process_add_administrator_tenant(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TenantAddAdministratorArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let tenant_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_add_administrator_tenant({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check account ownership
    assert_eq!(
        tenant_account.owner, program_id,
        "Invalid Tenant Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );

    // Parse accounts
    let globalstate = GlobalState::try_from(globalstate_account)?;
    let mut tenant = Tenant::try_from(tenant_account)?;

    // Check authorization: only foundation allowlist members can add administrators
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Check if administrator already exists
    if tenant.administrators.contains(&value.administrator) {
        return Err(DoubleZeroError::AdministratorAlreadyExists.into());
    }

    // Add the administrator
    tenant.administrators.push(value.administrator);

    // Write updated tenant
    try_acc_write(&tenant, tenant_account, payer_account, accounts)?;

    Ok(())
}
