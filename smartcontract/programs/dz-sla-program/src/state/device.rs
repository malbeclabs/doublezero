use crate::{bytereader::ByteReader, seeds::SEED_DEVICE, types::*};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;
use std::fmt;
use serde::Serialize;

use super::accounttype::{AccountType, AccountTypeInfo};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum DeviceType {
    Switch = 0,
}

impl From<u8> for DeviceType {
    fn from(value: u8) -> Self {
        match value {
            0 => DeviceType::Switch,
            _ => DeviceType::Switch, // Default case
        }
    }
}

impl fmt::Display for DeviceType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DeviceType::Switch => write!(f, "switch"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum DeviceStatus {
    Pending = 0,
    Activated = 1,
    Suspended = 2,
    Deleting = 3,
    Rejected = 4,
}

impl From<u8> for DeviceStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => DeviceStatus::Pending,
            1 => DeviceStatus::Activated,
            2 => DeviceStatus::Suspended,
            3 => DeviceStatus::Deleting,
            4 => DeviceStatus::Rejected,
            _ => DeviceStatus::Pending,
        }
    }
}

impl fmt::Display for DeviceStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            DeviceStatus::Pending => write!(f, "pending"),
            DeviceStatus::Activated => write!(f, "activated"),
            DeviceStatus::Suspended => write!(f, "suspended"),
            DeviceStatus::Deleting => write!(f, "deleting"),
            DeviceStatus::Rejected => write!(f, "rejected"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone, Serialize)]
pub struct Device {
    pub account_type: AccountType,  // 1
    pub owner: Pubkey,              // 32
    pub index: u128,                // 16
    pub tenant_pk: Pubkey,          // 32
    pub location_pk: Pubkey,        // 32
    pub exchange_pk: Pubkey,        // 32
    pub device_type: DeviceType,    // 1
    pub public_ip: IpV4,            // 4
    pub status: DeviceStatus,       // 1
    pub code: String,               // 4 + len
    pub dz_prefixes: NetworkV4List, // 4 + 5 * len
}

impl fmt::Display for Device {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, location_pk: {}, exchange_pk: {}, device_type: {}, public_ip: {}, dz_prefixes: {}, status: {}, code: {}",
            self.account_type, self.owner, self.index, self.location_pk, self.exchange_pk, self.device_type, ipv4_to_string(&self.public_ip), networkv4_list_to_string(&self.dz_prefixes), self.status, self.code
        )
    }
}

impl AccountTypeInfo for Device {
    fn seed(&self) -> &[u8] {
        SEED_DEVICE
    }
    fn size(&self) -> usize {
        1 + 32 + 16 + 32 + 32 + 32 + 1 + 4 + 1 + 4 + self.code.len() + 4 + 5 * self.dz_prefixes.len()
    }
    fn index(&self) -> u128 {
        self.index
    }
    fn owner(&self) -> Pubkey {
        self.owner
    }
}

impl From<&[u8]> for Device {
    fn from(data: &[u8]) -> Self {
        let mut parser = ByteReader::new(data);

        let device = Self {
            account_type: parser.read_enum(),
            owner: parser.read_pubkey(),
            index: parser.read_u128(),
            tenant_pk: parser.read_pubkey(),
            location_pk: parser.read_pubkey(),
            exchange_pk: parser.read_pubkey(),
            device_type: parser.read_enum(),
            public_ip: parser.read_ipv4(),
            status: parser.read_enum(),
            code: parser.read_string(),
            dz_prefixes: parser.read_networkv4_vec(),
        };

        device
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_exchange_serialization() {
        let val = Device {
            account_type: AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 123,
            code: "test-321".to_string(),
            device_type: DeviceType::Switch,
            tenant_pk: Pubkey::default(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            dz_prefixes: vec![([10, 0, 0, 1], 24), ([11, 0, 0, 1], 24)],
            public_ip: ipv4_parse(&"1.2.3.4".to_string()),
            status: DeviceStatus::Activated,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Device::from(&data[..]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.dz_prefixes, val2.dz_prefixes);
        assert_eq!(val.location_pk, val2.location_pk);
        assert_eq!(val.exchange_pk, val2.exchange_pk);
        assert_eq!(val.public_ip, val2.public_ip);
        assert_eq!(val.dz_prefixes, val2.dz_prefixes);
        assert_eq!(val.status, val2.status);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
