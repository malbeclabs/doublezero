use core::fmt;

use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

use crate::pda::*;
use crate::types::*;
use crate::{
    error::DoubleZeroError,
    helper::*,
    state::{accounttype::AccountType, device::*},
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct DeviceCreateArgs {
    pub index: u128,
    pub code: String,
    pub location_pk: Pubkey,
    pub exchange_pk: Pubkey,
    pub device_type: DeviceType,
    pub public_ip: IpV4,
    pub dz_prefixes: NetworkV4List,
}

impl fmt::Debug for DeviceCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, location_pk: {}, exchange_pk: {}, device_type: {:?}, public_ip: {}, dz_prefixes: {}",
            self.code, self.location_pk, self.exchange_pk, self.device_type, ipv4_to_string(&self.public_ip), networkv4_list_to_string(&self.dz_prefixes)
        )
    }
}

pub fn process_create_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let location_account = next_account_info(accounts_iter)?;
    let exchange_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_device({:?})", value);

    if !pda_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    if globalstate_account.data.borrow().is_empty() {
        panic!("GlobalState account not initialized");
    }
    let globalstate = globalstate_get_next(globalstate_account)?;
    assert_eq!(
        value.index, globalstate.account_index,
        "Invalid Value Index"
    );

    if !globalstate.device_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (expected_pda_account, bump_seed) = get_device_pda(program_id, globalstate.account_index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid Device PubKey"
    );

    // Check account Types
    if location_account.data_is_empty()
        || location_account.data.borrow()[0] != AccountType::Location as u8
    {
        return Err(DoubleZeroError::InvalidLocationPubkey.into());
    }
    if location_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    if exchange_account.data_is_empty()
        || exchange_account.data.borrow()[0] != AccountType::Exchange as u8
    {
        return Err(DoubleZeroError::InvalidExchangePubkey.into());
    }
    if exchange_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let device: Device = Device {
        account_type: AccountType::Device,
        owner: *payer_account.key,
        index: globalstate.account_index,
        code: value.code.clone(),
        location_pk: value.location_pk,
        exchange_pk: value.exchange_pk,
        device_type: value.device_type,
        public_ip: value.public_ip,
        dz_prefixes: value.dz_prefixes.clone(),
        status: DeviceStatus::Pending,
        tunnel_count: 0,
        user_count: 0,
    };

    account_create(
        pda_account,
        &device,
        payer_account,
        system_program,
        program_id,
        bump_seed,
    )?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
