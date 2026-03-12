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

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct ReserveConnectionArgs {
    #[incremental(default = 1)]
    pub count: u16,
}

impl fmt::Debug for ReserveConnectionArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "count: {}", self.count)
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

    // Validate count > 0
    if value.count == 0 {
        return Err(DoubleZeroError::InvalidArgument.into());
    }

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

    // Check device capacity: always check overall max_users, and additionally
    // check per-type multicast limits when configured (> 0).
    let new_reserved = device
        .reserved_seats
        .checked_add(value.count)
        .ok_or(DoubleZeroError::MaxUsersExceeded)?;
    let total_occupied = device
        .users_count
        .checked_add(new_reserved)
        .ok_or(DoubleZeroError::MaxUsersExceeded)?;
    if total_occupied > device.max_users {
        return Err(DoubleZeroError::MaxUsersExceeded.into());
    }
    if device.max_multicast_subscribers > 0 {
        let total_subscribers = device
            .multicast_subscribers_count
            .checked_add(new_reserved)
            .ok_or(DoubleZeroError::MaxMulticastSubscribersExceeded)?;
        if total_subscribers > device.max_multicast_subscribers {
            return Err(DoubleZeroError::MaxMulticastSubscribersExceeded.into());
        }
    }

    // Validate reservation PDA (keyed by device + owner)
    let (expected_pda, bump_seed) =
        get_reservation_pda(program_id, device_account.key, payer_account.key);
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
        reserved_count: value.count,
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
            payer_account.key.as_ref(),
            &[bump_seed],
        ],
    )?;

    // Increment reserved seats on device
    device.reserved_seats = new_reserved;
    try_acc_write(&device, device_account, payer_account, accounts)?;

    Ok(())
}
