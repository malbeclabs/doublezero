use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    resource::{IdOrIp, ResourceType},
    serializer::try_acc_close,
    state::{
        globalstate::GlobalState, resource_extension::ResourceExtensionBorrowed, tenant::Tenant,
    },
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
pub struct TenantDeleteArgs {}

impl fmt::Debug for TenantDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "TenantDeleteArgs")
    }
}

pub fn process_delete_tenant(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &TenantDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let tenant_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let vrf_ids_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_tenant");

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

    // Validate VRF IDs resource extension account
    assert_eq!(
        vrf_ids_account.owner, program_id,
        "Invalid ResourceExtension Account Owner for VrfIds"
    );
    assert!(
        vrf_ids_account.is_writable,
        "ResourceExtension Account for VrfIds is not writable"
    );
    assert!(
        !vrf_ids_account.data_is_empty(),
        "ResourceExtension Account for VrfIds is empty"
    );

    let (expected_vrf_ids_pda, _, _) = get_resource_extension_pda(program_id, ResourceType::VrfIds);
    assert_eq!(
        vrf_ids_account.key, &expected_vrf_ids_pda,
        "Invalid ResourceExtension PDA for VrfIds"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;

    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Parse the tenant account
    let tenant = Tenant::try_from(tenant_account)?;

    // Check if the tenant has any references
    if tenant.reference_count != 0 {
        return Err(DoubleZeroError::ReferenceCountNotZero.into());
    }

    // Deallocate VRF ID back to the ResourceExtension
    {
        let mut buffer = vrf_ids_account.data.borrow_mut();
        let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
        resource.deallocate(&IdOrIp::Id(tenant.vrf_id));
    }

    // Close the tenant account
    try_acc_close(tenant_account, payer_account)?;

    Ok(())
}
