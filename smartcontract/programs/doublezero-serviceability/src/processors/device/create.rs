use core::fmt;

use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get_next, globalstate_write},
    helper::*,
    pda::*,
    state::{accounttype::AccountType, device::*},
    types::*,
};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct DeviceCreateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub code: String,
    pub contributor_pk: Pubkey,
    pub location_pk: Pubkey,
    pub exchange_pk: Pubkey,
    pub device_type: DeviceType,
    pub public_ip: std::net::Ipv4Addr,
    pub dz_prefixes: NetworkV4List,
    pub metrics_publisher_pk: Pubkey,
}

impl fmt::Debug for DeviceCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, location_pk: {}, exchange_pk: {}, device_type: {:?}, public_ip: {}, dz_prefixes: {}, metrics_publisher_pk: {}",
            self.code, self.location_pk, self.exchange_pk, self.device_type, &self.public_ip, &self.dz_prefixes, self.metrics_publisher_pk
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
    let contributor_account = next_account_info(accounts_iter)?;
    let location_account = next_account_info(accounts_iter)?;
    let exchange_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_device({:?})", value);

    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        location_account.owner, program_id,
        "Invalid Location Account Owner"
    );
    assert_eq!(
        exchange_account.owner, program_id,
        "Invalid Exchange Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );

    if !pda_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    if globalstate_account.data.borrow().is_empty() {
        return Err(ProgramError::UninitializedAccount);
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
    assert!(bump_seed == value.bump_seed, "Invalid Device Bump Seed");

    // Check account Types
    if contributor_account.data_is_empty()
        || contributor_account.data.borrow()[0] != AccountType::Contributor as u8
    {
        return Err(DoubleZeroError::InvalidLocationPubkey.into());
    }
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
        bump_seed,
        code: value.code.clone(),
        contributor_pk: value.contributor_pk,
        location_pk: value.location_pk,
        exchange_pk: value.exchange_pk,
        device_type: value.device_type,
        public_ip: value.public_ip,
        dz_prefixes: value.dz_prefixes.clone(),
        metrics_publisher_pk: value.metrics_publisher_pk,
        status: DeviceStatus::Pending,
    };

    account_create(
        pda_account,
        &device,
        payer_account,
        system_program,
        program_id,
    )?;

    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
