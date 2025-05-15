use crate::bytereader::ByteReader;
use crate::{seeds::SEED_USER, types::*};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
use solana_program::account_info::AccountInfo;
use solana_program::pubkey::Pubkey;
use std::fmt;

use super::accounttype::{AccountType, AccountTypeInfo};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum UserType {
    IBRL = 0,
    IBRLWithAllocatedIP = 1,
    EdgeFiltering = 2,
    Multicast = 3,
}

impl From<u8> for UserType {
    fn from(value: u8) -> Self {
        match value {
            0 => UserType::IBRL,
            1 => UserType::IBRLWithAllocatedIP,
            2 => UserType::EdgeFiltering,
            3 => UserType::Multicast,
            _ => panic!("Unknown UserType"),
        }
    }
}

impl fmt::Display for UserType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            UserType::IBRL => write!(f, "IBRL"),
            UserType::IBRLWithAllocatedIP => write!(f, "IBRLWithAllocatedIP"),
            UserType::EdgeFiltering => write!(f, "EdgeFiltering"),
            UserType::Multicast => write!(f, "Multicast"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum UserCYOA {
    None = 0,
    GREOverDIA = 1,
    GREOverFabric = 2,
    GREOverPrivatePeering = 3,
    GREOverPublicPeering = 4,
    GREOverCable = 5,
}

impl From<u8> for UserCYOA {
    fn from(value: u8) -> Self {
        match value {
            1 => UserCYOA::GREOverDIA,
            2 => UserCYOA::GREOverFabric,
            3 => UserCYOA::GREOverPrivatePeering,
            4 => UserCYOA::GREOverPublicPeering,
            5 => UserCYOA::GREOverCable,
            _ => UserCYOA::None,
        }
    }
}

impl fmt::Display for UserCYOA {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            UserCYOA::None => write!(f, "none"),
            UserCYOA::GREOverDIA => write!(f, "GREOverDIA"),
            UserCYOA::GREOverFabric => write!(f, "GREOverFabric"),
            UserCYOA::GREOverPrivatePeering => write!(f, "GREOverPrivatePeering"),
            UserCYOA::GREOverPublicPeering => write!(f, "GREOverPublicPeering"),
            UserCYOA::GREOverCable => write!(f, "GREOverCable"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum UserStatus {
    Pending = 0,
    Activated = 1,
    Suspended = 2,
    Deleting = 3,
    Rejected = 4,
    PendingBan = 5,
    Banned = 6,
    Updating = 7,
}

impl From<u8> for UserStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => UserStatus::Pending,
            1 => UserStatus::Activated,
            2 => UserStatus::Suspended,
            3 => UserStatus::Deleting,
            4 => UserStatus::Rejected,
            5 => UserStatus::PendingBan,
            6 => UserStatus::Banned,
            7 => UserStatus::Updating,
            _ => UserStatus::Pending,
        }
    }
}

impl fmt::Display for UserStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            UserStatus::Pending => write!(f, "pending"),
            UserStatus::Activated => write!(f, "activated"),
            UserStatus::Suspended => write!(f, "suspended"),
            UserStatus::Deleting => write!(f, "deleting"),
            UserStatus::Rejected => write!(f, "rejected"),
            UserStatus::PendingBan => write!(f, "pending ban"),
            UserStatus::Updating => write!(f, "updating"),
            UserStatus::Banned => write!(f, "banned"),
        }
    }
}
#[derive(BorshSerialize, Debug, PartialEq, Clone, Serialize)]
pub struct User {
    pub account_type: AccountType, // 1
    pub owner: Pubkey,             // 32
    pub index: u128,               // 16
    pub bump_seed: u8,             // 1
    pub user_type: UserType,       // 1
    pub tenant_pk: Pubkey,         // 32
    pub device_pk: Pubkey,         // 32
    pub cyoa_type: UserCYOA,       // 1
    pub client_ip: IpV4,           // 4
    pub dz_ip: IpV4,               // 4
    pub tunnel_id: u16,            // 2
    pub tunnel_net: NetworkV4,     // 5
    pub status: UserStatus,        // 1
    pub publishers: Vec<Pubkey>,   // 4 + 32 * len
    pub subscribers: Vec<Pubkey>,  // 4 + 32 * len
}

impl fmt::Display for User {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, user_type: {}, device_pk: {}, cyoa_type: {}, client_ip: {}, dz_ip: {}, tunnel_id: {}, tunnel_net: {}, status: {}",
            self.account_type,
            self.owner,
            self.index,
            self.user_type,
            self.device_pk,
            self.cyoa_type,
            ipv4_to_string(&self.client_ip),
            ipv4_to_string(&self.dz_ip),
            self.tunnel_id,
            networkv4_to_string(&self.tunnel_net),
            self.status
        )
    }
}

impl AccountTypeInfo for User {
    fn seed(&self) -> &[u8] {
        SEED_USER
    }
    fn size(&self) -> usize {
        1 + 32
            + 16
            + 1
            + 1
            + 32
            + 32
            + 1
            + 4
            + 4
            + 2
            + 5
            + 1
            + 4
            + self.publishers.len() * 32
            + 4
            + self.subscribers.len() * 32
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

impl From<&[u8]> for User {
    fn from(data: &[u8]) -> Self {
        let mut parser = ByteReader::new(data);

        Self {
            account_type: parser.read_enum(),
            owner: parser.read_pubkey(),
            index: parser.read_u128(),
            bump_seed: parser.read_u8(),
            user_type: parser.read_enum(),
            tenant_pk: parser.read_pubkey(),
            device_pk: parser.read_pubkey(),
            cyoa_type: parser.read_enum(),
            client_ip: parser.read_ipv4(),
            dz_ip: parser.read_ipv4(),
            tunnel_id: parser.read_u16(),
            tunnel_net: parser.read_networkv4(),
            status: parser.read_enum(),
            publishers: parser.read_pubkey_vec(),
            subscribers: parser.read_pubkey_vec(),
        }
    }
}

impl From<&AccountInfo<'_>> for User {
    fn from(account: &AccountInfo) -> Self {
        let data = account.try_borrow_data().unwrap();
        Self::from(&data[..])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_user_serialization() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: ipv4_parse(&"3.2.4.2".to_string()),
            client_ip: ipv4_parse(&"1.2.3.4".to_string()),
            tunnel_id: 0,
            tunnel_net: networkv4_parse(&"10.0.0.1/25".to_string()),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = User::from(&data[..]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.device_pk, val2.device_pk);
        assert_eq!(val.dz_ip, val2.dz_ip);
        assert_eq!(val.client_ip, val2.client_ip);
        assert_eq!(val.tunnel_net, val2.tunnel_net);
        assert_eq!(val.subscribers, val2.subscribers);
        assert_eq!(val.publishers, val2.publishers);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
