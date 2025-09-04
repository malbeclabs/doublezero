use crate::{
    error::{DoubleZeroError, Validate},
    helper::deserialize_vec_with_capacity,
    seeds::SEED_MULTICAST_GROUP,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum MulticastGroupStatus {
    #[default]
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

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct MulticastGroup {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub index: u128,               // 16
    pub bump_seed: u8,             // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub tenant_pk: Pubkey, // 32
    pub multicast_ip: Ipv4Addr,    // 4
    pub max_bandwidth: u64,        // 8
    pub status: MulticastGroupStatus, // 1
    pub code: String,              // 4 + len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkeylist_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkeylist_from_string"
        )
    )]
    pub pub_allowlist: Vec<Pubkey>, // 4 + 32 * len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkeylist_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkeylist_from_string"
        )
    )]
    pub sub_allowlist: Vec<Pubkey>, // 4 + 32 * len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkeylist_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkeylist_from_string"
        )
    )]
    pub publishers: Vec<Pubkey>, // 4 + 32 * len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkeylist_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkeylist_from_string"
        )
    )]
    pub subscribers: Vec<Pubkey>, // 4 + 32 * len
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

impl TryFrom<&[u8]> for MulticastGroup {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            index: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            tenant_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            multicast_ip: BorshDeserialize::deserialize(&mut data).unwrap_or([0, 0, 0, 0].into()),
            max_bandwidth: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            code: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            pub_allowlist: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            sub_allowlist: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            publishers: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            subscribers: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::MulticastGroup {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for MulticastGroup {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Self::try_from(&data[..])
    }
}

impl Validate for MulticastGroup {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be MulticastGroup
        if self.account_type != AccountType::MulticastGroup {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        // Multicast IP must be in the range
        if self.status != MulticastGroupStatus::Pending && !self.multicast_ip.is_multicast() {
            msg!("Invalid multicast IP: {}", self.multicast_ip);
            return Err(DoubleZeroError::InvalidMulticastIp);
        }
        if self.max_bandwidth == 0 {
            msg!("Invalid max bandwidth: {}", self.max_bandwidth);
            return Err(DoubleZeroError::InvalidMaxBandwidth);
        }
        // Code must be less than or equal to 32 bytes
        if self.code.len() > 32 {
            msg!("Code too long: {}", self.code.len());
            return Err(DoubleZeroError::CodeTooLong);
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    #[test]
    fn test_state_multicastgroup_validate_error_invalid_account_type() {
        // Should fail because account_type is not MulticastGroup
        let val = MulticastGroup {
            account_type: AccountType::Device, // Should be MulticastGroup
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 1].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            code: "test".to_string(),
            pub_allowlist: vec![Pubkey::new_unique()],
            sub_allowlist: vec![Pubkey::new_unique()],
            publishers: vec![Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique()],
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }
    use super::*;

    #[test]
    fn test_state_multicastgroup_serialization() {
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
        let val2 = MulticastGroup::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

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
