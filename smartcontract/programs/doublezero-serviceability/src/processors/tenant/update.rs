use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, tenant::Tenant},
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
pub struct TenantUpdateArgs {
    pub vrf_id: Option<u16>,
    pub token_account: Option<Pubkey>,
    pub metro_route: Option<bool>,
    pub route_liveness: Option<bool>,
}

impl fmt::Debug for TenantUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "vrf_id: {:?}, token_account: {:?}, metro_route: {:?}, route_liveness: {:?}",
            self.vrf_id, self.token_account, self.metro_route, self.route_liveness
        )
    }
}

pub fn process_update_tenant(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TenantUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let tenant_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_tenant({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        tenant_account.owner, program_id,
        "Invalid Tenant Account Owner"
    );
    // Check if the account is writable
    assert!(tenant_account.is_writable, "Tenant Account is not writable");

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;

    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Parse the tenant account
    let mut tenant = Tenant::try_from(tenant_account)?;

    // Update the fields if provided
    // Note: code and owner cannot be updated (code is used for PDA derivation, owner is immutable)
    if let Some(vrf_id) = value.vrf_id {
        tenant.vrf_id = vrf_id;
    }
    if let Some(token_account) = value.token_account {
        tenant.token_account = token_account;
    }
    if let Some(metro_route) = value.metro_route {
        tenant.metro_route = metro_route;
    }
    if let Some(route_liveness) = value.route_liveness {
        tenant.route_liveness = route_liveness;
    }
    try_acc_write(&tenant, tenant_account, payer_account, accounts)?;

    Ok(())
}
