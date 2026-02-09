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
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone, Default)]
pub struct UpdatePaymentStatusArgs {
    pub payment_status: u8,
}

pub fn process_update_payment_status(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UpdatePaymentStatusArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let tenant_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

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

    // Parse the global state account & check if the payer is sentinel or foundation
    let globalstate = GlobalState::try_from(globalstate_account)?;

    if globalstate.sentinel_authority_pk != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        msg!(
            "sentinel_authority_pk: {} payer: {} foundation_allowlist: {:?}",
            globalstate.sentinel_authority_pk,
            payer_account.key,
            globalstate.foundation_allowlist
        );
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Validate payment status range
    if value.payment_status > 3 {
        msg!("Invalid payment status: {}", value.payment_status);
        return Err(DoubleZeroError::InvalidPaymentStatus.into());
    }

    // Parse the tenant account
    let mut tenant = Tenant::try_from(tenant_account)?;

    // Update the payment status
    tenant.payment_status = value.payment_status;

    try_acc_write(&tenant, tenant_account, payer_account, accounts)?;

    Ok(())
}
