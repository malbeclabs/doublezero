use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
use solana_program::pubkey::Pubkey;
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, Clone, Copy, Debug, Default, PartialEq)]
pub enum ResourceType {
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

impl IdOrIp {
    pub fn as_ip(&self) -> Option<NetworkV4> {
        match self {
            IdOrIp::Ip(ip) => Some(*ip),
            IdOrIp::Id(_) => None,
        }
    }

    pub fn as_id(&self) -> Option<u16> {
        match self {
            IdOrIp::Ip(_) => None,
            IdOrIp::Id(id) => Some(*id),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_as_ip() {
        let ip = "192.168.1.1/32".parse::<NetworkV4>().unwrap();
        let id_or_ip = IdOrIp::Ip(ip);
        assert_eq!(id_or_ip.as_ip(), Some(ip));
        let id_or_ip = IdOrIp::Id(42);
        assert_eq!(id_or_ip.as_ip(), None);
    }

    #[test]
    fn test_as_id() {
        let id_or_ip = IdOrIp::Id(1234);
        assert_eq!(id_or_ip.as_id(), Some(1234));
        let ip = "192.168.1.1/32".parse::<NetworkV4>().unwrap();
        let id_or_ip = IdOrIp::Ip(ip);
        assert_eq!(id_or_ip.as_id(), None);
    }
}
