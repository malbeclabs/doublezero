use std::net::Ipv4Addr;

use solana_program::pubkey::Pubkey;

use crate::seeds::{
    SEED_ACCESS_PASS, SEED_CONFIG, SEED_CONTRIBUTOR, SEED_DEVICE, SEED_EXCHANGE, SEED_GLOBALSTATE,
    SEED_LINK, SEED_LOCATION, SEED_MULTICAST_GROUP, SEED_PREFIX, SEED_PROGRAM_CONFIG, SEED_USER,
};

pub fn get_globalstate_pda(program_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_GLOBALSTATE], program_id)
}

pub fn get_globalconfig_pda(program_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_CONFIG], program_id)
}

pub fn get_program_config_pda(program_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_PROGRAM_CONFIG], program_id)
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

pub fn get_link_pda(program_id: &Pubkey, index: u128) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_LINK, &index.to_le_bytes()], program_id)
}

pub fn get_user_pda(program_id: &Pubkey, index: u128) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_USER, &index.to_le_bytes()], program_id)
}

pub fn get_multicastgroup_pda(program_id: &Pubkey, index: u128) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[SEED_PREFIX, SEED_MULTICAST_GROUP, &index.to_le_bytes()],
        program_id,
    )
}

pub fn get_contributor_pda(program_id: &Pubkey, index: u128) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[SEED_PREFIX, SEED_CONTRIBUTOR, &index.to_le_bytes()],
        program_id,
    )
}

pub fn get_accesspass_pda(
    program_id: &Pubkey,
    client_ip: &Ipv4Addr,
    payer: &Pubkey,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            SEED_PREFIX,
            SEED_ACCESS_PASS,
            &client_ip.octets(),
            &payer.to_bytes(),
        ],
        program_id,
    )
}
