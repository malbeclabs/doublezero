use crate::{
    bytereader::ByteReader,
    seeds::SEED_CONTRIBUTOR,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum ContributorType {
    Network = 0,
}

impl From<u8> for ContributorType {
    fn from(value: u8) -> Self {
        match value {
            0 => ContributorType::Network,
            _ => ContributorType::Network, // Default case
        }
    }
}

impl fmt::Display for ContributorType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ContributorType::Network => write!(f, "network"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum ContributorStatus {
    None = 0,
    Activated = 1,
    Suspended = 2,
    Deleting = 3,
}

impl From<u8> for ContributorStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => ContributorStatus::None,
            1 => ContributorStatus::Activated,
            2 => ContributorStatus::Suspended,
            3 => ContributorStatus::Deleting,
            _ => ContributorStatus::None,
        }
    }
}

impl fmt::Display for ContributorStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ContributorStatus::None => write!(f, "none"),
            ContributorStatus::Activated => write!(f, "activated"),
            ContributorStatus::Suspended => write!(f, "suspended"),
            ContributorStatus::Deleting => write!(f, "deleting"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Contributor {
    pub account_type: AccountType, // 1
    pub owner: Pubkey,             // 32
    pub index: u128,               // 16
    pub bump_seed: u8,             // 1
    pub status: ContributorStatus, // 1
    pub code: String,              // 4 + len
    pub reference_count: u32,      // 4
}

impl fmt::Display for Contributor {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, bump_seed: {}, code: {}",
            self.account_type, self.owner, self.index, self.bump_seed, self.code
        )
    }
}

impl AccountTypeInfo for Contributor {
    fn seed(&self) -> &[u8] {
        SEED_CONTRIBUTOR
    }
    fn size(&self) -> usize {
        1 + 32 + 16 + 1 + 1 + 4 + self.code.len() + 4
    }
    fn bump_seed(&self) -> u8 {
        self.bump_seed
    }
    fn index(&self) -> u128 {
        self.index
    }
    fn owner(&self) -> Pubkey {
        self.owner
    }
}

impl From<&[u8]> for Contributor {
    fn from(data: &[u8]) -> Self {
        let mut parser = ByteReader::new(data);

        Self {
            account_type: parser.read_enum(),
            owner: parser.read_pubkey(),
            index: parser.read_u128(),
            bump_seed: parser.read_u8(),
            status: parser.read_enum(),
            code: parser.read_string(),
            reference_count: parser.read_u32(),
        }
    }
}

impl TryFrom<&AccountInfo<'_>> for Contributor {
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
    fn test_state_contributor_serialization() {
        let val = Contributor {
            account_type: AccountType::Contributor,
            owner: Pubkey::default(),
            index: 123,
            bump_seed: 1,
            reference_count: 0,
            status: ContributorStatus::Activated,
            code: "test".to_string(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Contributor::from(&data[..]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.index, val2.index);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.status, val2.status);
        assert_eq!(val.account_type, val2.account_type);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
