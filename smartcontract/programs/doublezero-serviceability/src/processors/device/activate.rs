use crate::{
    error::DoubleZeroError,
    processors::resource::create_resource,
    resource::ResourceType,
    serializer::try_acc_write,
    state::{device::*, globalstate::GlobalState},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeviceActivateArgs {
    pub resource_count: usize,
}

impl fmt::Debug for DeviceActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "DeviceActivateArgs {{ resource_count: {} }}",
            self.resource_count
        )
    }
}

pub fn process_activate_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceActivateArgs,
) -> ProgramResult {
    assert!(
        value.resource_count >= 2,
        "Resource count must be at least 2 (TunnelIds and at least one DzPrefixBlock)"
    );

    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let globalconfig_account = next_account_info(accounts_iter)?;
    let mut resource_accounts = vec![];
    for _ in 0..value.resource_count {
        // first resource account is the TunnelIds resource, followed by DzPrefixBlock resources
        let resource_account = next_account_info(accounts_iter)?;
        resource_accounts.push(resource_account);
    }
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_device({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    if device_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    if globalstate_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    if device.status != DeviceStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    //TODO: This should be changed once the Health Oracle is finalized.
    //device.status = DeviceStatus::DeviceProvisioning;
    device.status = DeviceStatus::Activated;

    device.check_status_transition();

    for (idx, resource_account) in resource_accounts.iter().enumerate() {
        create_resource(
            program_id,
            resource_account,
            Some(device_account),
            globalconfig_account,
            payer_account,
            accounts,
            match idx {
                0 => ResourceType::TunnelIds(*device_account.key, 0),
                _ => ResourceType::DzPrefixBlock(*device_account.key, idx - 1),
            },
        )?;
    }

    try_acc_write(&device, device_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Activated: {:?}", device);

    Ok(())
}
