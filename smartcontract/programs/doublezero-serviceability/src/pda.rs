use std::net::Ipv4Addr;

use solana_program::pubkey::Pubkey;

use crate::{
    seeds::{
        SEED_ACCESS_PASS, SEED_CONFIG, SEED_CONTRIBUTOR, SEED_DEVICE, SEED_DEVICE_TUNNEL_BLOCK,
        SEED_DZ_PREFIX_BLOCK, SEED_EXCHANGE, SEED_GLOBALSTATE, SEED_LINK, SEED_LINK_IDS,
        SEED_LOCATION, SEED_MULTICASTGROUP_BLOCK, SEED_MULTICAST_GROUP, SEED_PREFIX,
        SEED_PROGRAM_CONFIG, SEED_SEGMENT_ROUTING_IDS, SEED_TENANT, SEED_TUNNEL_IDS, SEED_USER,
        SEED_USER_TUNNEL_BLOCK, SEED_VRF_IDS,
    },
    state::user::UserType,
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

pub fn get_user_old_pda(program_id: &Pubkey, index: u128) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_USER, &index.to_le_bytes()], program_id)
}

pub fn get_user_pda(program_id: &Pubkey, ip: &Ipv4Addr, user_type: UserType) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[SEED_PREFIX, SEED_USER, &ip.octets(), &[user_type as u8]],
        program_id,
    )
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

pub fn get_tenant_pda(program_id: &Pubkey, code: &str) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_TENANT, code.as_bytes()], program_id)
}

pub fn get_accesspass_pda(
    program_id: &Pubkey,
    client_ip: &Ipv4Addr,
    user_payer: &Pubkey,
) -> (Pubkey, u8) {
    Pubkey::find_program_address(
        &[
            SEED_PREFIX,
            SEED_ACCESS_PASS,
            &client_ip.octets(),
            &user_payer.to_bytes(),
        ],
        program_id,
    )
}

pub fn get_resource_extension_pda(
    program_id: &Pubkey,
    resource_type: crate::resource::ResourceType,
) -> (Pubkey, u8, &'static [u8]) {
    match resource_type {
        crate::resource::ResourceType::DeviceTunnelBlock => {
            let (pda, bump_seed) =
                Pubkey::find_program_address(&[SEED_PREFIX, SEED_DEVICE_TUNNEL_BLOCK], program_id);
            (pda, bump_seed, SEED_DEVICE_TUNNEL_BLOCK)
        }
        crate::resource::ResourceType::UserTunnelBlock => {
            let (pda, bump_seed) =
                Pubkey::find_program_address(&[SEED_PREFIX, SEED_USER_TUNNEL_BLOCK], program_id);
            (pda, bump_seed, SEED_USER_TUNNEL_BLOCK)
        }
        crate::resource::ResourceType::MulticastGroupBlock => {
            let (pda, bump_seed) =
                Pubkey::find_program_address(&[SEED_PREFIX, SEED_MULTICASTGROUP_BLOCK], program_id);
            (pda, bump_seed, SEED_MULTICASTGROUP_BLOCK)
        }
        crate::resource::ResourceType::DzPrefixBlock(ref associated_pk, index) => {
            let (pda, bump_seed) = Pubkey::find_program_address(
                &[
                    SEED_PREFIX,
                    SEED_DZ_PREFIX_BLOCK,
                    &associated_pk.to_bytes(),
                    &index.to_le_bytes(),
                ],
                program_id,
            );
            (pda, bump_seed, SEED_DZ_PREFIX_BLOCK)
        }
        crate::resource::ResourceType::TunnelIds(ref associated_pk, index) => {
            let (pda, bump_seed) = Pubkey::find_program_address(
                &[
                    SEED_PREFIX,
                    SEED_TUNNEL_IDS,
                    &associated_pk.to_bytes(),
                    &index.to_le_bytes(),
                ],
                program_id,
            );
            (pda, bump_seed, SEED_TUNNEL_IDS)
        }
        crate::resource::ResourceType::LinkIds => {
            let (pda, bump_seed) =
                Pubkey::find_program_address(&[SEED_PREFIX, SEED_LINK_IDS], program_id);
            (pda, bump_seed, SEED_LINK_IDS)
        }
        crate::resource::ResourceType::SegmentRoutingIds => {
            let (pda, bump_seed) =
                Pubkey::find_program_address(&[SEED_PREFIX, SEED_SEGMENT_ROUTING_IDS], program_id);
            (pda, bump_seed, SEED_SEGMENT_ROUTING_IDS)
        }
        crate::resource::ResourceType::VrfIds => {
            let (pda, bump_seed) =
                Pubkey::find_program_address(&[SEED_PREFIX, SEED_VRF_IDS], program_id);
            (pda, bump_seed, SEED_VRF_IDS)
        }
    }
}
