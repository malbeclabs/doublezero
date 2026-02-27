use crate::{
    error::DoubleZeroError,
    pda::*,
    seeds::{SEED_PREFIX, SEED_RESERVATION},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType, device::Device, globalstate::GlobalState,
        reservation::Reservation,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct ReserveConnectionArgs {
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr,
}

impl fmt::Debug for ReserveConnectionArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "client_ip: {}", self.client_ip)
    }
}

pub fn process_reserve_connection(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ReserveConnectionArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let reservation_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    // Validate signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate account owners
    assert_eq!(
        device_account.owner, program_id,
        "Invalid Device Account Owner"
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

    // Validate GlobalState PDA
    let (expected_globalstate, _) = get_globalstate_pda(program_id);
    assert_eq!(
        globalstate_account.key, &expected_globalstate,
        "Invalid GlobalState PubKey"
    );

    // Load global state and check authority
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if globalstate.reservation_authority_pk != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Load and validate device
    let mut device = Device::try_from(device_account)?;

    // Check device capacity: users_count + reserved_seats < max_users
    if device.max_users > 0 && device.users_count + device.reserved_seats >= device.max_users {
        return Err(DoubleZeroError::MaxUsersExceeded.into());
    }

    // Validate reservation PDA
    let (expected_pda, bump_seed) =
        get_reservation_pda(program_id, device_account.key, &value.client_ip);
    assert_eq!(
        reservation_account.key, &expected_pda,
        "Invalid Reservation PubKey"
    );

    // Create the reservation account
    let reservation = Reservation {
        account_type: AccountType::Reservation,
        owner: *payer_account.key,
        bump_seed,
        device_pk: *device_account.key,
        client_ip: value.client_ip,
    };

    try_acc_create(
        &reservation,
        reservation_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_RESERVATION,
            device_account.key.as_ref(),
            &value.client_ip.octets(),
            &[bump_seed],
        ],
    )?;

    // Increment reserved seats on device
    device.reserved_seats += 1;
    try_acc_write(&device, device_account, payer_account, accounts)?;

    Ok(())
}
