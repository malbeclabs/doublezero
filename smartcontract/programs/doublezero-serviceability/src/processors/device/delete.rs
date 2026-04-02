use crate::{
    error::DoubleZeroError,
    processors::validation::validate_program_account,
    serializer::{try_acc_close, try_acc_write},
    state::{
        accounttype::AccountType, contributor::Contributor, device::*, exchange::Exchange,
        globalstate::GlobalState, location::Location,
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
pub struct DeviceDeleteArgs {
    #[incremental(default = 0)]
    pub resource_count: u8,
}

impl fmt::Debug for DeviceDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "resource_count: {}", self.resource_count)
    }
}

pub fn process_delete_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Account layout WITH atomic close (resource_count > 0):
    //   [device, contributor, globalstate, location, exchange, resource_0..N, res_owner_0..N, owner, payer, system]
    // Account layout WITHOUT (legacy, resource_count == 0):
    //   [device, contributor, globalstate, payer, system]
    let atomic_accounts = if value.resource_count > 0 {
        let location_account = next_account_info(accounts_iter)?;
        let exchange_account = next_account_info(accounts_iter)?;

        let mut resource_accounts = Vec::with_capacity(value.resource_count as usize);
        for _ in 0..value.resource_count {
            resource_accounts.push(next_account_info(accounts_iter)?);
        }
        let mut res_owner_accounts = Vec::with_capacity(value.resource_count as usize);
        for _ in 0..value.resource_count {
            res_owner_accounts.push(next_account_info(accounts_iter)?);
        }

        let owner_account = next_account_info(accounts_iter)?;
        Some((
            location_account,
            exchange_account,
            resource_accounts,
            res_owner_accounts,
            owner_account,
        ))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_device({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(
        device_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "Device"
    );
    validate_program_account!(
        contributor_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "Contributor"
    );
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "GlobalState"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let mut contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let device: Device = Device::try_from(device_account)?;

    if matches!(
        device.status,
        DeviceStatus::Activated | DeviceStatus::Deleting
    ) {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    if device.reference_count > 0 {
        return Err(DoubleZeroError::ReferenceCountNotZero.into());
    }

    if !device.interfaces.is_empty() {
        return Err(DoubleZeroError::DeviceHasInterfaces.into());
    }

    if let Some((
        location_account,
        exchange_account,
        resource_accounts,
        res_owner_accounts,
        owner_account,
    )) = atomic_accounts
    {
        // Validate additional account owners
        assert_eq!(
            location_account.owner, program_id,
            "Invalid Location Account Owner"
        );
        assert_eq!(
            exchange_account.owner, program_id,
            "Invalid Exchange Account Owner"
        );

        // Validate device references match accounts
        if device.location_pk != *location_account.key {
            return Err(DoubleZeroError::InvalidLocationPubkey.into());
        }
        if device.exchange_pk != *exchange_account.key {
            return Err(DoubleZeroError::InvalidExchangePubkey.into());
        }
        if device.contributor_pk != *contributor_account.key {
            return Err(DoubleZeroError::InvalidContributorPubkey.into());
        }
        if device.owner != *owner_account.key {
            return Err(DoubleZeroError::InvalidOwnerPubkey.into());
        }

        // Validate resource/owner accounts
        for account in resource_accounts.iter() {
            assert_eq!(account.owner, program_id, "Invalid Resource Account Owner");
            assert!(account.is_writable, "Resource Account is not writable");
        }

        // Decrement reference counts
        let mut location = Location::try_from(location_account)?;
        let mut exchange = Exchange::try_from(exchange_account)?;

        contributor.reference_count = contributor.reference_count.saturating_sub(1);
        location.reference_count = location.reference_count.saturating_sub(1);
        exchange.reference_count = exchange.reference_count.saturating_sub(1);

        try_acc_write(&contributor, contributor_account, payer_account, accounts)?;
        try_acc_write(&location, location_account, payer_account, accounts)?;
        try_acc_write(&exchange, exchange_account, payer_account, accounts)?;
        try_acc_close(device_account, owner_account)?;

        for (resource_account, res_owner_account) in
            resource_accounts.iter().zip(res_owner_accounts.iter())
        {
            try_acc_close(resource_account, res_owner_account)?;
        }

        #[cfg(test)]
        msg!("DeleteDevice (atomic): Device closed");
    } else {
        // Legacy path: just mark as Deleting
        let mut device: Device = Device::try_from(device_account)?;
        device.status = DeviceStatus::Deleting;

        try_acc_write(&device, device_account, payer_account, accounts)?;

        #[cfg(test)]
        msg!("Deleting: {:?}", device);
    }

    Ok(())
}
