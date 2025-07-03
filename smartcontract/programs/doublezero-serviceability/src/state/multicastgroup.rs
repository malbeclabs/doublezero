use crate::{
    bytereader::ByteReader,
    seeds::SEED_MULTICAST_GROUP,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
use solana_program::{account_info::AccountInfo, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Serialize)]
#[borsh(use_discriminant = true)]
pub enum MulticastGroupStatus {
    Pending = 0,
    Activated = 1,
    Suspended = 2,
    Deleting = 3,
    Rejected = 4,
}

impl From<u8> for MulticastGroupStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => MulticastGroupStatus::Pending,
            1 => MulticastGroupStatus::Activated,
            2 => MulticastGroupStatus::Suspended,
            3 => MulticastGroupStatus::Deleting,
            4 => MulticastGroupStatus::Rejected,
            _ => MulticastGroupStatus::Pending,
        }
    }
}

impl fmt::Display for MulticastGroupStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            MulticastGroupStatus::Pending => write!(f, "pending"),
            MulticastGroupStatus::Activated => write!(f, "activated"),
            MulticastGroupStatus::Suspended => write!(f, "suspended"),
            MulticastGroupStatus::Deleting => write!(f, "deleting"),
            MulticastGroupStatus::Rejected => write!(f, "rejected"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone, Serialize)]
pub struct MulticastGroup {
    pub account_type: AccountType,    // 1
    pub owner: Pubkey,                // 32
    pub index: u128,                  // 16
    pub bump_seed: u8,                // 1
    pub tenant_pk: Pubkey,            // 32
    pub multicast_ip: Ipv4Addr,       // 4
    pub max_bandwidth: u64,           // 8
    pub status: MulticastGroupStatus, // 1
    pub code: String,                 // 4 + len
    pub pub_allowlist: Vec<Pubkey>,   // 4 + 32 * len
    pub sub_allowlist: Vec<Pubkey>,   // 4 + 32 * len
    pub publishers: Vec<Pubkey>,      // 4 + 32 * len
    pub subscribers: Vec<Pubkey>,     // 4 + 32 * len
}

impl fmt::Display for MulticastGroup {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, bump_seed:{}, code: {}, multicast_ip: {}, max_bandwdith: {}, status: {}",
            self.account_type, self.owner, self.index, self.bump_seed, self.code, &self.multicast_ip, self.max_bandwidth,  self.status,
        )
    }
}

impl AccountTypeInfo for MulticastGroup {
    fn seed(&self) -> &[u8] {
        SEED_MULTICAST_GROUP
    }
    fn size(&self) -> usize {
        1 + 32
            + 16
            + 1
            + 32
            + 4
            + 8
            + 4
            + self.code.len()
            + 4
            + self.pub_allowlist.len() * 32
            + 4
            + self.sub_allowlist.len() * 32
            + 4
            + self.publishers.len() * 32
            + 4
            + self.subscribers.len() * 32
            + 1
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

impl From<&[u8]> for MulticastGroup {
    fn from(data: &[u8]) -> Self {
        let mut parser = ByteReader::new(data);

        Self {
            account_type: parser.read_enum(),
            owner: parser.read_pubkey(),
            index: parser.read_u128(),
            bump_seed: parser.read_u8(),
            tenant_pk: parser.read_pubkey(),
            multicast_ip: parser.read_ipv4(),
            max_bandwidth: parser.read_u64(),
            status: parser.read_enum(),
            code: parser.read_string(),
            pub_allowlist: parser.read_pubkey_vec(),
            sub_allowlist: parser.read_pubkey_vec(),
            publishers: parser.read_pubkey_vec(),
            subscribers: parser.read_pubkey_vec(),
        }
    }
}

impl TryFrom<&AccountInfo<'_>> for MulticastGroup {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Ok(Self::from(&data[..]))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_location_serialization() {
        let val = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 1].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            code: "test".to_string(),
            pub_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            sub_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = MulticastGroup::from(&data[..]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.index, val2.index);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.tenant_pk, val2.tenant_pk);
        assert_eq!(val.multicast_ip, val2.multicast_ip);
        assert_eq!(val.status, val2.status);
        assert_eq!(val.account_type, val2.account_type);
        assert_eq!(val.max_bandwidth, val2.max_bandwidth);
        assert_eq!(val.account_type as u8, data[0], "Invalid Account Type");
        assert_eq!(
            val.account_type as u8, val2.account_type as u8,
            "Invalid Account Type"
        );
        assert_eq!(
            val.pub_allowlist.len(),
            val2.pub_allowlist.len(),
            "Invalid Pub Allowlist"
        );
        assert_eq!(
            val.sub_allowlist.len(),
            val2.sub_allowlist.len(),
            "Invalid Sub Allowlist"
        );
        assert_eq!(
            val.publishers.len(),
            val2.publishers.len(),
            "Invalid Publishers"
        );
        assert_eq!(
            val.subscribers.len(),
            val2.subscribers.len(),
            "Invalid Subscribers"
        );
        assert_eq!(
            val.pub_allowlist[0], val2.pub_allowlist[0],
            "Invalid Pub Allowlist"
        );
        assert_eq!(
            val.sub_allowlist[0], val2.sub_allowlist[0],
            "Invalid Sub Allowlist"
        );
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
