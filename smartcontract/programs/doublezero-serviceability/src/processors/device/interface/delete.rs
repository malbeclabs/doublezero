use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{
        accounttype::AccountType, contributor::Contributor, device::*, globalstate::GlobalState,
        interface::InterfaceStatus,
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
pub struct DeviceInterfaceDeleteArgs {
    pub name: String,
}

impl fmt::Debug for DeviceInterfaceDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "name: {}", self.name)
    }
}

pub fn process_delete_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_device_interface({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        device_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
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
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    let (idx, _) = device
        .find_interface(&value.name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;
    let mut iface = device.interfaces[idx].into_current_version();

    if iface.status != InterfaceStatus::Activated && iface.status != InterfaceStatus::Unlinked {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    iface.status = InterfaceStatus::Deleting;
    device.interfaces[idx] = iface.to_interface();

    try_acc_write(&device, device_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Deleting interface: {} from {:?}", value.name, device);

    Ok(())
}
