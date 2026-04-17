use crate::{error::Validate, state::accounttype::AccountType};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{program_error::ProgramError, pubkey::Pubkey};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Clone, Copy, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum TopologyConstraint {
    #[default]
    IncludeAny = 0,
    Exclude = 1,
}

#[derive(BorshSerialize, BorshDeserialize, Debug, Clone, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct TopologyInfo {
    pub account_type: AccountType,
    pub owner: Pubkey,
    pub bump_seed: u8,
    pub name: String,         // max 32 bytes enforced on create
    pub admin_group_bit: u8,  // 0–127
    pub flex_algo_number: u8, // always 128 + admin_group_bit
    pub constraint: TopologyConstraint,
    pub reference_count: u32,
}

impl std::fmt::Display for TopologyInfo {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "name={} bit={} algo={} color={} constraint={:?} reference_count={}",
            self.name,
            self.admin_group_bit,
            self.flex_algo_number,
            self.admin_group_bit as u16 + 1,
            self.constraint,
            self.reference_count
        )
    }
}

impl TryFrom<&[u8]> for TopologyInfo {
    type Error = solana_program::program_error::ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            name: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            admin_group_bit: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            flex_algo_number: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            constraint: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            reference_count: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::Topology {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&solana_program::account_info::AccountInfo<'_>> for TopologyInfo {
    type Error = solana_program::program_error::ProgramError;

    fn try_from(account: &solana_program::account_info::AccountInfo) -> Result<Self, Self::Error> {
        Self::try_from(&account.data.borrow()[..])
    }
}

impl Validate for TopologyInfo {
    fn validate(&self) -> Result<(), crate::error::DoubleZeroError> {
        if self.account_type != AccountType::Topology {
            return Err(crate::error::DoubleZeroError::InvalidAccountType);
        }
        if self.name.len() > 32 {
            return Err(crate::error::DoubleZeroError::NameTooLong);
        }
        Ok(())
    }
}

/// Flex-algo node segment entry on a Vpnv4 loopback Interface account.
#[derive(BorshSerialize, BorshDeserialize, Debug, Clone, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct FlexAlgoNodeSegment {
    pub topology: Pubkey,      // TopologyInfo PDA pubkey
    pub node_segment_idx: u16, // allocated from SegmentRoutingIds ResourceExtension
}
