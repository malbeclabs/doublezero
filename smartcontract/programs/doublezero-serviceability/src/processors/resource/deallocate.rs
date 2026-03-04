use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    resource::{IdOrIp, ResourceType},
    state::{globalstate::GlobalState, resource_extension::ResourceExtensionBorrowed},
};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct ResourceDeallocateArgs {
    pub resource_type: ResourceType,
    pub value: IdOrIp,
}

impl TryFrom<&[u8]> for ResourceDeallocateArgs {
    type Error = DoubleZeroError;

    fn try_from(mut value: &[u8]) -> Result<Self, DoubleZeroError> {
        ResourceDeallocateArgs::deserialize(&mut value)
            .map_err(|_| DoubleZeroError::InvalidArgument)
    }
}

impl fmt::Debug for ResourceDeallocateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "ResourceDeallocateArgs {{ resource_type: {:?}, value: {:?} }}",
            self.resource_type, self.value
        )
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
        resource_account.owner, program_id,
        "Invalid Resource Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(resource_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (expected_resource_pda, _, _) = get_resource_extension_pda(program_id, value.resource_type);
    assert_eq!(
        resource_account.key, &expected_resource_pda,
        "Invalid Resource Account PubKey"
    );

    match value.resource_type {
        ResourceType::DzPrefixBlock(ref associated_pk, _)
        | ResourceType::TunnelIds(ref associated_pk, _) => {
            assert_eq!(
                associated_account.key, associated_pk,
                "Associated account pubkeys do not match"
            );
        }
        _ => {}
    }

    assert!(!resource_account.data_is_empty());
    assert_eq!(
        resource_account.owner, program_id,
        "Invalid Resource Account Owner"
    );

    let mut buffer = resource_account.data.borrow_mut();
    let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;

    resource.deallocate(&value.value);

    Ok(())
}
