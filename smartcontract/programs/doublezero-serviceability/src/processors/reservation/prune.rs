use crate::{
    error::DoubleZeroError,
    pda::*,
    serializer::try_acc_write,
    state::{
        device::Device,
        globalstate::GlobalState,
        reservation::{Reservation, ReservationStatus},
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
pub struct PruneReservationArgs {}

impl fmt::Debug for PruneReservationArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_prune_reservation(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &PruneReservationArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let reservation_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    // Validate signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate account owners
    assert_eq!(
        reservation_account.owner, program_id,
        "Invalid Reservation Account Owner"
    );
    assert_eq!(
        device_account.owner, program_id,
        "Invalid Device Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
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

    // Load reservation and validate status
    let mut reservation = Reservation::try_from(reservation_account)?;
    if reservation.status != ReservationStatus::Reserved {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // Validate that reservation belongs to this device
    if reservation.device_pk != *device_account.key {
        return Err(DoubleZeroError::InvalidDevicePubkey.into());
    }

    // Update reservation status
    reservation.status = ReservationStatus::Pruned;
    try_acc_write(&reservation, reservation_account, payer_account, accounts)?;

    // Decrement reserved seats on device
    let mut device = Device::try_from(device_account)?;
    device.reserved_seats = device.reserved_seats.saturating_sub(1);
    try_acc_write(&device, device_account, payer_account, accounts)?;

    Ok(())
}
