use super::accounttype::{AccountType, AccountTypeInfo};
use crate::{bytereader::ByteReader, seeds::SEED_LINK, types::*};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
use solana_program::{account_info::AccountInfo, pubkey::Pubkey};
use std::{fmt, str::FromStr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum LinkLinkType {
    L1 = 1,
    L2 = 2,
    L3 = 3,
}

impl From<u8> for LinkLinkType {
    fn from(value: u8) -> Self {
        match value {
            1 => LinkLinkType::L1,
            2 => LinkLinkType::L2,
            3 => LinkLinkType::L3,
            _ => LinkLinkType::L2, // Default case
        }
    }
}

impl FromStr for LinkLinkType {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "L1" => Ok(LinkLinkType::L1),
            "L2" => Ok(LinkLinkType::L2),
            "L3" => Ok(LinkLinkType::L3),
            _ => Err(format!("Invalid LinkLinkType: {}", s)),
        }
    }
}

impl fmt::Display for LinkLinkType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LinkLinkType::L1 => write!(f, "L1"),
            LinkLinkType::L2 => write!(f, "L2"),
            LinkLinkType::L3 => write!(f, "L3"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum LinkStatus {
    Pending = 0,
    Activated = 1,
    Suspended = 2,
    Deleting = 3,
    Rejected = 4,
}

impl From<u8> for LinkStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => LinkStatus::Pending,
            1 => LinkStatus::Activated,
            2 => LinkStatus::Suspended,
            3 => LinkStatus::Deleting,
            4 => LinkStatus::Rejected,
            _ => LinkStatus::Pending,
        }
    }
}

impl fmt::Display for LinkStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            LinkStatus::Pending => write!(f, "pending"),
            LinkStatus::Activated => write!(f, "activated"),
            LinkStatus::Suspended => write!(f, "suspended"),
            LinkStatus::Deleting => write!(f, "deleting"),
            LinkStatus::Rejected => write!(f, "rejected"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone, Serialize)]
pub struct Link {
    pub account_type: AccountType, // 1
    pub owner: Pubkey,             // 32
    pub index: u128,               // 16
    pub bump_seed: u8,             // 1
    pub side_a_pk: Pubkey,         // 32
    pub side_z_pk: Pubkey,         // 32
    pub link_type: LinkLinkType,   // 1
    pub bandwidth: u64,            // 8
    pub mtu: u32,                  // 4
    pub delay_ns: u64,             // 8
    pub jitter_ns: u64,            // 8
    pub tunnel_id: u16,            // 2
    pub tunnel_net: NetworkV4,     // 5 (IP(4 x u8) + Prefix (u8) CIDR)
    pub status: LinkStatus,        // 1
    pub code: String,              // 4 + len
}

impl fmt::Display for Link {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, side_a_pk: {}, side_z_pk: {}, tunnel_type: {}, bandwidth: {}, mtu: {}, delay_ns: {}, jitter_ns: {}, tunnel_id: {}, tunnel_net: {}, status: {}, code: {}",
            self.account_type, self.owner, self.index, self.side_a_pk, self.side_z_pk, self.link_type, self.bandwidth, self.mtu, self.delay_ns, self.jitter_ns, self.tunnel_id, networkv4_to_string(&self.tunnel_net), self.status, self.code
        )
    }
}

impl AccountTypeInfo for Link {
    fn seed(&self) -> &[u8] {
        SEED_LINK
    }
    fn size(&self) -> usize {
        1 + 32 + 16 + 1 + 32 + 32 + 1 + 8 + 4 + 8 + 8 + 2 + 5 + 1 + 4 + self.code.len()
    }
    fn index(&self) -> u128 {
        self.index
    }
    fn bump_seed(&self) -> u8 {
        self.bump_seed
    }
    fn owner(&self) -> Pubkey {
        self.owner
    }
}

impl From<&[u8]> for Link {
    fn from(data: &[u8]) -> Self {
        let mut parser = ByteReader::new(data);

        Self {
            account_type: parser.read_enum(),
            owner: parser.read_pubkey(),
            index: parser.read_u128(),
            bump_seed: parser.read_u8(),
            side_a_pk: parser.read_pubkey(),
            side_z_pk: parser.read_pubkey(),
            link_type: parser.read_enum(),
            bandwidth: parser.read_u64(),
            mtu: parser.read_u32(),
            delay_ns: parser.read_u64(),
            jitter_ns: parser.read_u64(),
            tunnel_id: parser.read_u16(),
            tunnel_net: parser.read_networkv4(),
            status: parser.read_enum(),
            code: parser.read_string(),
        }
    }
}

impl From<&AccountInfo<'_>> for Link {
    fn from(account: &AccountInfo) -> Self {
        let data = account.try_borrow_data().unwrap();
        Self::from(&data[..])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_tunnel_serialization() {
        let val = Link {
            account_type: AccountType::Link,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            link_type: LinkLinkType::L3,
            bandwidth: 1234,
            mtu: 1566,
            delay_ns: 1234,
            jitter_ns: 1121,
            tunnel_id: 1234,
            tunnel_net: networkv4_parse("1.2.3.4/32"),
            code: "test-123".to_string(),
            status: LinkStatus::Activated,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Link::from(&data[..]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.side_a_pk, val2.side_a_pk);
        assert_eq!(val.side_z_pk, val2.side_z_pk);
        assert_eq!(val.mtu, val2.mtu);
        assert_eq!(val.bandwidth, val2.bandwidth);
        assert_eq!(val.tunnel_net, val2.tunnel_net);
        assert_eq!(val.code, val2.code);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
