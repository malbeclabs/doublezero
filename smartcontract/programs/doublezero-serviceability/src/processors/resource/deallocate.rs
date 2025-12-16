use crate::{
    error::DoubleZeroError, globalstate::globalstate_get, pda::get_resource_extension_pda,
    resource::IpBlockType, state::resource_extension::ResourceExtensionBorrowed,
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct ResourceDeallocateArgs {
    pub ip_block_type: IpBlockType,
    pub network: NetworkV4,
}

impl fmt::Debug for ResourceDeallocateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "ResourceDeallocateArgs {{}}",)
    }
}

pub fn process_deallocate_resource(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ResourceDeallocateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let resource_account = next_account_info(accounts_iter)?;
    let associated_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_deallocate_resource({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

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
    assert!(resource_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (expected_resource_pda, _, _) = get_resource_extension_pda(program_id, value.ip_block_type);
    assert_eq!(
        resource_account.key, &expected_resource_pda,
        "Invalid Resource Account PubKey"
    );

    if let crate::resource::IpBlockType::DzPrefixBlock(ref associated_pk, _) = value.ip_block_type {
        assert_eq!(
            associated_account.key, associated_pk,
            "Associated account pubkeys do not match"
        );
    }

    assert!(!resource_account.data.borrow().is_empty());
    assert_eq!(
        resource_account.owner, program_id,
        "Invalid Resource Account Owner"
    );

    let mut buffer = resource_account.data.borrow_mut();
    let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;

    resource.deallocate(&value.network);

    Ok(())
}
