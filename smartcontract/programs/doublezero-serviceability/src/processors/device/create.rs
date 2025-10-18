use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get_next, globalstate_write},
    helper::{account_create, account_write},
    pda::get_device_pda,
    state::{
        accounttype::AccountType, contributor::Contributor, device::*, exchange::Exchange,
        location::Location,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::{types::NetworkV4List, validate_account_code};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct DeviceCreateArgs {
    pub code: String,
    pub device_type: DeviceType,
    #[incremental(default = std::net::Ipv4Addr::UNSPECIFIED)]
    pub public_ip: std::net::Ipv4Addr,
    pub dz_prefixes: NetworkV4List,
    pub metrics_publisher_pk: Pubkey,
    pub mgmt_vrf: String,
}

impl fmt::Debug for DeviceCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, device_type: {:?}, public_ip: {}, dz_prefixes: {}, \
metrics_publisher_pk: {}, mgmt_vrf: {}",
            self.code,
            self.device_type,
            self.public_ip,
            self.dz_prefixes,
            self.metrics_publisher_pk,
            self.mgmt_vrf,
        )
    }
}

pub fn process_create_device(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let location_account = next_account_info(accounts_iter)?;
    let exchange_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_device({:?})", value);

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

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

    if !device_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    let globalstate = globalstate_get_next(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let mut contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    let (expected_pda_account, bump_seed) = get_device_pda(program_id, globalstate.account_index);
    assert_eq!(
        device_account.key, &expected_pda_account,
        "Invalid Device PubKey"
    );

    let mut location = Location::try_from(location_account)?;
    let mut exchange = Exchange::try_from(exchange_account)?;

    contributor.reference_count += 1;
    location.reference_count += 1;
    exchange.reference_count += 1;

    let device: Device = Device {
        account_type: AccountType::Device,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        reference_count: 0,
        code,
        contributor_pk: *contributor_account.key,
        location_pk: *location_account.key,
        exchange_pk: *exchange_account.key,
        device_type: value.device_type,
        public_ip: value.public_ip,
        dz_prefixes: value.dz_prefixes.clone(),
        metrics_publisher_pk: value.metrics_publisher_pk,
        status: DeviceStatus::Pending,
        mgmt_vrf: value.mgmt_vrf.clone(),
        interfaces: vec![],
        users_count: 0,
        max_users: 0, // Initially, the Device is locked and must be activated by modifying the maximum number of users.
    };

    account_create(
        device_account,
        &device,
        payer_account,
        system_program,
        program_id,
    )?;
    account_write(
        contributor_account,
        &contributor,
        payer_account,
        system_program,
    )?;
    account_write(location_account, &location, payer_account, system_program)?;
    account_write(exchange_account, &exchange, payer_account, system_program)?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
