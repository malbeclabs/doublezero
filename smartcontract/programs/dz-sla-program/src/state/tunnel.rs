use std::{fmt, str::FromStr};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;
use crate::{bytereader::ByteReader, seeds::SEED_TUNNEL, types::*};

use super::accounttype::{AccountType, AccountTypeInfo};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant=true)]
pub enum TunnelTunnelType {
    MPLSoGRE = 1,
}

impl From<u8> for TunnelTunnelType {
    fn from(value: u8) -> Self {
        match value {
            1 => TunnelTunnelType::MPLSoGRE,
            _ => TunnelTunnelType::MPLSoGRE, // Default case
        }
    }
}

impl FromStr for TunnelTunnelType {
    type Err = String; 

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s {
            "MPLSoGRE" => Ok(TunnelTunnelType::MPLSoGRE),
            _ => Err(format!("Invalid TunnelTunnelType: {}", s)),
        }
    }
}


impl fmt::Display for TunnelTunnelType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            TunnelTunnelType::MPLSoGRE => write!(f, "MPLSoGRE"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant=true)]
pub enum TunnelStatus {
    Pending = 0,
    Activated = 1,
    Suspended = 2,
    Deleting = 3,
    Rejected = 4,
}

impl From<u8> for TunnelStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => TunnelStatus::Pending,
            1 => TunnelStatus::Activated,
            2 => TunnelStatus::Suspended,
            3 => TunnelStatus::Deleting,
            4 => TunnelStatus::Rejected,
            _ => TunnelStatus::Pending,
        }
    }
}

impl fmt::Display for TunnelStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            TunnelStatus::Pending => write!(f, "pending"),
            TunnelStatus::Activated => write!(f, "activated"),
            TunnelStatus::Suspended => write!(f, "suspended"),
            TunnelStatus::Deleting => write!(f, "deleting"),
            TunnelStatus::Rejected => write!(f, "rejected"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
pub struct Tunnel {
    pub account_type: AccountType,      // 1
    pub owner: Pubkey,                  // 32
    pub index: u128,                    // 16
    pub side_a_pk: Pubkey,              // 32
    pub side_z_pk: Pubkey,              // 32
    pub tunnel_type: TunnelTunnelType,  // 1
    pub bandwidth: u64,                 // 8
    pub mtu: u32,                       // 4
    pub delay_ns: u64,                  // 8
    pub jitter_ns: u64,                 // 8
    pub tunnel_id: u16,                 // 2
    pub tunnel_net: NetworkV4,          // 5 (IP(4 x u8) + Prefix (u8) CIDR)
    pub status: TunnelStatus,           // 1
    pub code: String,                   // 4 + len
}

impl AccountTypeInfo for Tunnel {
    fn seed(&self) -> &[u8] { SEED_TUNNEL }
    fn size(&self) -> usize { 
        1 + 32 + 16 + 32 + 32 + 1 + 8 + 4 + 8 + 8 + 2 + 5 + 1 + 4 + self.code.len()
    }
    fn index(&self) -> u128 { self.index }
    fn owner(&self) -> Pubkey { self.owner }
}

impl From<&[u8]> for Tunnel {
    fn from(data: &[u8]) -> Self {

        let mut parser = ByteReader::new(data);

        Self {
            account_type: parser.read_enum(),
            owner: parser.read_pubkey(),
            index: parser.read_u128(),
            side_a_pk: parser.read_pubkey(),
            side_z_pk: parser.read_pubkey(),
            tunnel_type: parser.read_enum(),
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



#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_tunnel_serialization() {

        let val = Tunnel {
            account_type: AccountType::Tunnel,
            owner: Pubkey::new_unique(),
            index: 123,
            side_a_pk: Pubkey::new_unique(),
            side_z_pk: Pubkey::new_unique(),
            tunnel_type: TunnelTunnelType::MPLSoGRE,
            bandwidth: 1234,
            mtu: 1566,
            delay_ns: 1234,
            jitter_ns: 1121,
            tunnel_id: 1234,
            tunnel_net: networkv4_parse(&"1.2.3.4/32".to_string()),
            code: "test-123".to_string(),           
            status: TunnelStatus::Activated,       
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Tunnel::from(&data[..]);

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