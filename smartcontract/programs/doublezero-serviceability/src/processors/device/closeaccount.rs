use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{contributor::Contributor, device::*, exchange::Exchange, location::Location},
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

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeviceCloseAccountArgs {}

impl fmt::Debug for DeviceCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_closeaccount_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &DeviceCloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let location_account = next_account_info(accounts_iter)?;
    let exchange_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_closeaccount_device({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        device_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        location_account.owner, program_id,
        "Invalid Location Owner Account"
    );
    assert_eq!(
        exchange_account.owner, program_id,
        "Invalid Exchange Owner Account"
    );

    assert!(device_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let device = Device::try_from(device_account)?;

    if device.status != DeviceStatus::Deleting {
        #[cfg(test)]
        msg!("{:?}", device);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }
    if device.location_pk != *location_account.key {
        return Err(DoubleZeroError::InvalidLocationPubkey.into());
    }
    if device.exchange_pk != *exchange_account.key {
        return Err(DoubleZeroError::InvalidExchangePubkey.into());
    }
    let mut contributor = Contributor::try_from(contributor_account)?;
    let mut location = Location::try_from(location_account)?;
    let mut exchange = Exchange::try_from(exchange_account)?;

    contributor.reference_count = contributor.reference_count.saturating_sub(1);
    location.reference_count = location.reference_count.saturating_sub(1);
    exchange.reference_count = exchange.reference_count.saturating_sub(1);

    account_write(
        contributor_account,
        &contributor,
        payer_account,
        system_program,
    )?;
    account_write(location_account, &location, payer_account, system_program)?;
    account_write(exchange_account, &exchange, payer_account, system_program)?;
    account_close(device_account, owner_account)?;

    #[cfg(test)]
    msg!("CloseAccount: Device closed");

    Ok(())
}
