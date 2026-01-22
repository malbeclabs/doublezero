use crate::{
    error::DoubleZeroError,
    serializer::{try_acc_close, try_acc_write},
    state::{
        contributor::Contributor, device::*, exchange::Exchange, globalstate::GlobalState,
        location::Location,
    },
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
pub struct DeviceCloseAccountArgs {
    pub resource_count: usize,
}

impl fmt::Debug for DeviceCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "DeviceCloseAccountArgs {{ resource_count: {} }}",
            self.resource_count
        )
    }
}

pub fn process_closeaccount_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceCloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let location_account = next_account_info(accounts_iter)?;
    let exchange_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let globalconfig_account = next_account_info(accounts_iter)?;
    let mut resource_accounts = vec![];
    let mut owner_accounts = vec![];
    for _ in 0..value.resource_count {
        // first resource account is the TunnelIds resource, followed by DzPrefixBlock resources
        let resource_account = next_account_info(accounts_iter)?;
        resource_accounts.push(resource_account);
    }
    for _ in 0..value.resource_count {
        // first resource account is the TunnelIds resource, followed by DzPrefixBlock resources
        let owner_account = next_account_info(accounts_iter)?;
        owner_accounts.push(owner_account);
    }
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_closeaccount_device({:?})", value);

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
        globalconfig_account.owner, program_id,
        "Invalid GlobalConfig Account Owner"
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

    for account in resource_accounts.iter() {
        assert_eq!(
            account.owner, program_id,
            "Invalid Resource/Owner Account Owner"
        );
        assert!(
            account.is_writable,
            "Resource/Owner Account is not writable"
        );
    }

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let device = Device::try_from(device_account)?;

    if device.status != DeviceStatus::Deleting {
        #[cfg(test)]
        msg!("{:?}", device);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }
    // This catches edge cases where reference_count could be incremented
    // after DeleteDevice but before CloseAccountDevice
    if device.reference_count > 0 {
        return Err(DoubleZeroError::ReferenceCountNotZero.into());
    }
    if device.location_pk != *location_account.key {
        return Err(DoubleZeroError::InvalidLocationPubkey.into());
    }
    if device.exchange_pk != *exchange_account.key {
        return Err(DoubleZeroError::InvalidExchangePubkey.into());
    }
    if device.contributor_pk != *contributor_account.key {
        return Err(DoubleZeroError::InvalidContributorPubkey.into());
    }
    let mut contributor = Contributor::try_from(contributor_account)?;
    let mut location = Location::try_from(location_account)?;
    let mut exchange = Exchange::try_from(exchange_account)?;

    contributor.reference_count = contributor.reference_count.saturating_sub(1);
    location.reference_count = location.reference_count.saturating_sub(1);
    exchange.reference_count = exchange.reference_count.saturating_sub(1);

    try_acc_write(&contributor, contributor_account, payer_account, accounts)?;
    try_acc_write(&location, location_account, payer_account, accounts)?;
    try_acc_write(&exchange, exchange_account, payer_account, accounts)?;
    try_acc_close(device_account, owner_account)?;

    for (resource_account, res_owner_account) in resource_accounts.iter().zip(owner_accounts.iter())
    {
        try_acc_close(resource_account, res_owner_account)?;
    }

    #[cfg(test)]
    msg!("CloseAccount: Device closed");

    Ok(())
}
