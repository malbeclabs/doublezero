use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
use solana_program::pubkey::Pubkey;
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, Clone, Copy, Debug, Default, PartialEq)]
pub enum ResourceBlockType {
    #[default]
    DeviceTunnelBlock,
    UserTunnelBlock,
    MulticastGroupBlock,
    DzPrefixBlock(Pubkey, usize),
    TunnelIds(Pubkey, usize),
    LinkIds,
    SegmentRoutingIds,
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Clone, Debug, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
#[borsh(use_discriminant = true)]
pub enum IdOrIp {
    Ip(NetworkV4),
    Id(u16),
}

impl fmt::Display for IdOrIp {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            IdOrIp::Ip(ip) => write!(f, "{}", ip),
            IdOrIp::Id(id) => write!(f, "{}", id),
        }
    }
}
