use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    resource::IdOrIp,
    state::{globalstate::GlobalState, resource_extension::ResourceExtensionBorrowed},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct ResourceAllocateArgs {
    pub resource_type: crate::resource::ResourceType,
    pub requested: Option<IdOrIp>,
}

impl fmt::Debug for ResourceAllocateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "ResourceAllocateArgs {{ resource_type: {:?}, requested: {:?} }}",
            self.resource_type, self.requested
        )
    }
}

pub fn process_allocate_resource(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ResourceAllocateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let resource_account = next_account_info(accounts_iter)?;
    let associated_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_allocate_resource({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        resource_account.owner, program_id,
        "Invalid Resource Account Owner"
    );
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

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    match value.resource_type {
        crate::resource::ResourceType::DzPrefixBlock(ref associated_pk, _)
        | crate::resource::ResourceType::TunnelIds(ref associated_pk, _) => {
            assert_eq!(
                associated_account.key, associated_pk,
                "Associated account pubkeys do not match"
            );
        }
        _ => {}
    }

    let (expected_resource_pda, _, _) = get_resource_extension_pda(program_id, value.resource_type);
    assert_eq!(
        resource_account.key, &expected_resource_pda,
        "Invalid Resource Account PubKey"
    );

    assert!(!resource_account.data_is_empty());
    assert_eq!(
        resource_account.owner, program_id,
        "Invalid Resource Account Owner"
    );

    let mut buffer = resource_account.data.borrow_mut();
    let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;

    if let Some(ref requested) = &value.requested {
        resource.allocate_specific(requested)?;
    } else {
        resource.allocate(1)?;
    }

    Ok(())
}
