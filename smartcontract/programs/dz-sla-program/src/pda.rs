use crate::seeds::*;
use solana_program::pubkey::Pubkey;

pub fn get_globalstate_pda(program_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_GLOBALSTATE], program_id)
}

pub fn get_globalconfig_pda(program_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_CONFIG], program_id)
}

pub fn get_location_pda(program_id: &Pubkey, index: u128) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[SEED_PREFIX, SEED_LOCATION, &index.to_le_bytes()],
        program_id,
    )
}

pub fn get_exchange_pda(program_id: &Pubkey, index: u128) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[SEED_PREFIX, SEED_EXCHANGE, &index.to_le_bytes()],
        program_id,
    )
}

pub fn get_device_pda(program_id: &Pubkey, index: u128) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[SEED_PREFIX, SEED_DEVICE, &index.to_le_bytes()],
        program_id,
    )
}

pub fn get_tunnel_pda(program_id: &Pubkey, index: u128) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[SEED_PREFIX, SEED_TUNNEL, &index.to_le_bytes()],
        program_id,
    )
}

pub fn get_user_pda(program_id: &Pubkey, index: u128) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_USER, &index.to_le_bytes()], program_id)
}
