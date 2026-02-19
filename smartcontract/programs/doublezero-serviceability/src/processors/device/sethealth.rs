use core::fmt;

use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{accounttype::AccountType, device::*, globalstate::GlobalState},
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

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeviceSetHealthArgs {
    pub health: DeviceHealth,
}

impl fmt::Debug for DeviceSetHealthArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "health: {:?}", self.health)
    }
}

pub fn process_set_health_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceSetHealthArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_health_device({:?})", value);
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
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(device_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    if globalstate.health_oracle_pk != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;
    device.device_health = value.health;
    device.check_status_transition();

    try_acc_write(&device, device_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Set health: {:?}", device);

    Ok(())
}
