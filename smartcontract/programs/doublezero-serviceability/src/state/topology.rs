use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{program_error::ProgramError, pubkey::Pubkey};

pub const MAX_TOPOLOGY_NAME_LEN: usize = 32;

/// Validate a topology name: non-empty, ≤32 bytes, ASCII uppercase
/// alphanumeric with dashes, and must start with a letter.
pub fn validate_topology_name(name: &str) -> Result<(), DoubleZeroError> {
    if name.is_empty() {
        return Err(DoubleZeroError::InvalidName);
    }
    if name.len() > MAX_TOPOLOGY_NAME_LEN {
        return Err(DoubleZeroError::NameTooLong);
    }
    let mut chars = name.chars();
    let first = chars.next().expect("non-empty checked above");
    if !first.is_ascii_uppercase() {
        return Err(DoubleZeroError::InvalidName);
    }
    for c in chars {
        if !(c.is_ascii_uppercase() || c.is_ascii_digit() || c == '-') {
            return Err(DoubleZeroError::InvalidName);
        }
    }
    Ok(())
}

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
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::Topology {
            return Err(DoubleZeroError::InvalidAccountType);
        }
        validate_topology_name(&self.name)
    }
}

/// Flex-algo node segment entry on a Vpnv4 loopback Interface account.
#[derive(BorshSerialize, BorshDeserialize, Debug, Clone, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct FlexAlgoNodeSegment {
    pub topology: Pubkey,      // TopologyInfo PDA pubkey
    pub node_segment_idx: u16, // allocated from SegmentRoutingIds ResourceExtension
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validate_topology_name_accepts_valid_names() {
        for name in [
            "UNICAST-DEFAULT",
            "SHELBY",
            "A",
            "A1",
            "A-B-C",
            "A9",
            "X-1-Y-2",
        ] {
            assert!(
                validate_topology_name(name).is_ok(),
                "expected '{name}' to be valid"
            );
        }
        // Exactly 32 chars is accepted.
        let max_len = "A".repeat(MAX_TOPOLOGY_NAME_LEN);
        assert!(validate_topology_name(&max_len).is_ok());
    }

    #[test]
    fn validate_topology_name_rejects_empty() {
        assert_eq!(
            validate_topology_name(""),
            Err(DoubleZeroError::InvalidName)
        );
    }

    #[test]
    fn validate_topology_name_rejects_too_long() {
        let too_long = "A".repeat(MAX_TOPOLOGY_NAME_LEN + 1);
        assert_eq!(
            validate_topology_name(&too_long),
            Err(DoubleZeroError::NameTooLong)
        );
    }

    #[test]
    fn validate_topology_name_rejects_lowercase() {
        assert_eq!(
            validate_topology_name("unicast-default"),
            Err(DoubleZeroError::InvalidName)
        );
        assert_eq!(
            validate_topology_name("a"),
            Err(DoubleZeroError::InvalidName)
        );
        assert_eq!(
            validate_topology_name("Mixed-Case"),
            Err(DoubleZeroError::InvalidName)
        );
    }

    #[test]
    fn validate_topology_name_rejects_bad_first_char() {
        assert_eq!(
            validate_topology_name("1ABC"),
            Err(DoubleZeroError::InvalidName)
        );
        assert_eq!(
            validate_topology_name("-ABC"),
            Err(DoubleZeroError::InvalidName)
        );
    }

    #[test]
    fn validate_topology_name_rejects_disallowed_chars() {
        for name in ["ABC_DEF", "ABC DEF", "ABC!", "ABC.DEF", "ABC/DEF"] {
            assert_eq!(
                validate_topology_name(name),
                Err(DoubleZeroError::InvalidName),
                "expected '{name}' to be rejected"
            );
        }
    }

    #[test]
    fn topologyinfo_validate_delegates_to_name_validator() {
        let mut info = TopologyInfo {
            account_type: AccountType::Topology,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            name: "UNICAST-DEFAULT".to_string(),
            admin_group_bit: 1,
            flex_algo_number: 129,
            constraint: TopologyConstraint::IncludeAny,
            reference_count: 0,
        };
        assert!(info.validate().is_ok());

        info.name = "unicast-default".to_string();
        assert_eq!(info.validate(), Err(DoubleZeroError::InvalidName));

        info.name = "UNICAST-DEFAULT".to_string();
        info.account_type = AccountType::Device;
        assert_eq!(info.validate(), Err(DoubleZeroError::InvalidAccountType));
    }
}
