use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

/// A deposit tier from RFC-28. The deposit is sized against the rate a builder commits to, so the
/// tier is what a stake buys: the highest rate a feed backed by it may commit to.
#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Eq, Default, Copy, Clone)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum StakeTier {
    /// No stake. Covers no rate, which is what an account that was never written must read as.
    #[default]
    None = 0,
    UpTo1Gbps = 1,
    UpTo5Gbps = 2,
    Unmetered = 3,
}

impl StakeTier {
    /// The highest committed rate this tier covers, in bits per second. Gbps is decimal here, as
    /// it is everywhere a link rate is quoted.
    pub fn max_rate_bits_per_sec(self) -> u64 {
        match self {
            StakeTier::None => 0,
            StakeTier::UpTo1Gbps => 1_000_000_000,
            StakeTier::UpTo5Gbps => 5_000_000_000,
            StakeTier::Unmetered => u64::MAX,
        }
    }

    /// Whether a feed committing to `rate_bits_per_sec` is covered by this tier. A zero rate is
    /// not a commitment, so no tier covers it.
    pub fn covers(self, rate_bits_per_sec: u64) -> bool {
        rate_bits_per_sec != 0 && rate_bits_per_sec <= self.max_rate_bits_per_sec()
    }
}

impl fmt::Display for StakeTier {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            StakeTier::None => "none",
            StakeTier::UpTo1Gbps => "up-to-1gbps",
            StakeTier::UpTo5Gbps => "up-to-5gbps",
            StakeTier::Unmetered => "unmetered",
        };
        write!(f, "{s}")
    }
}

/// A builder's Solana stake, copied onto the DZ ledger.
///
/// A DZ ledger program cannot read a Solana account, so a relayer watches `builder-stake` on
/// Solana and writes what it saw here. This is an assertion, not a proof: it is worth exactly as
/// much as the key that signed it, which is recorded in `relayer`. A relayer can lie in both
/// directions, so who is allowed to sign one is a trust decision, not an implementation detail.
///
/// One mirror per builder. `builder` is the PDA seed, so it is immutable.
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone, Default)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct StakeMirror {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    /// Whoever created the account and paid its rent. Not the same question as who vouched for the
    /// values in it, which is `relayer`.
    pub owner: Pubkey, // 32
    pub bump_seed: u8,             // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    /// The builder that posted the stake, and the PDA seed.
    pub builder: Pubkey, // 32
    /// The tier the deposit bought. This is what a feed's committed rate is checked against.
    pub tier: StakeTier, // 1
    /// The rate the builder declared when it deposited, which is what the deposit was sized
    /// against. Kept for the record; the coverage check reads `tier`.
    pub committed_rate_bits_per_sec: u64, // 8
    /// The Solana slot of the `builder-stake` event this reflects. A write carrying a slot no newer
    /// than the stored one is a repeat, so the mirror cannot be walked backwards by a replay.
    pub source_slot: u64, // 8
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    /// The authority whose signature admitted the current values. A reader that does not trust this
    /// key should not trust the stake.
    pub relayer: Pubkey, // 32
}

impl fmt::Display for StakeMirror {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, bump_seed: {}, builder: {}, tier: {}, committed_rate_bits_per_sec: {}, source_slot: {}, relayer: {}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.builder,
            self.tier,
            self.committed_rate_bits_per_sec,
            self.source_slot,
            self.relayer
        )
    }
}

impl TryFrom<&[u8]> for StakeMirror {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            builder: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            tier: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            committed_rate_bits_per_sec: BorshDeserialize::deserialize(&mut data)
                .unwrap_or_default(),
            source_slot: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            relayer: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::StakeMirror {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for StakeMirror {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!(
                "Failed to deserialize StakeMirror: {:?}",
                res.as_ref().err()
            );
        }
        res
    }
}

impl Validate for StakeMirror {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::StakeMirror {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        if self.builder == Pubkey::default() {
            msg!("StakeMirror must name a builder");
            return Err(DoubleZeroError::InvalidArgument);
        }
        if self.relayer == Pubkey::default() {
            msg!("StakeMirror must record the relayer that asserted it");
            return Err(DoubleZeroError::InvalidArgument);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn mirror(tier: StakeTier) -> StakeMirror {
        StakeMirror {
            account_type: AccountType::StakeMirror,
            owner: Pubkey::new_unique(),
            bump_seed: 254,
            builder: Pubkey::new_unique(),
            tier,
            committed_rate_bits_per_sec: 1_000_000_000,
            source_slot: 123_456,
            relayer: Pubkey::new_unique(),
        }
    }

    #[test]
    fn test_stake_mirror_serialization_roundtrip() {
        let val = mirror(StakeTier::UpTo5Gbps);
        let data = borsh::to_vec(&val).unwrap();
        let val2 = StakeMirror::try_from(&data[..]).unwrap();
        val.validate().unwrap();
        val2.validate().unwrap();
        assert_eq!(val, val2);
        assert_eq!(data.len(), borsh::object_length(&val).unwrap());
    }

    #[test]
    fn test_stake_mirror_wrong_account_type_rejected() {
        let mut val = mirror(StakeTier::UpTo1Gbps);
        val.account_type = AccountType::Feed;
        let data = borsh::to_vec(&val).unwrap();
        assert!(StakeMirror::try_from(&data[..]).is_err());
    }

    #[test]
    fn test_stake_mirror_requires_builder_and_relayer() {
        let mut val = mirror(StakeTier::UpTo1Gbps);
        val.builder = Pubkey::default();
        assert!(val.validate().is_err());

        let mut val = mirror(StakeTier::UpTo1Gbps);
        val.relayer = Pubkey::default();
        assert!(val.validate().is_err());
    }

    /// An account that was never written decodes as tier None, which covers nothing. A missing
    /// mirror and an empty one therefore reach create-feed as the same answer.
    #[test]
    fn test_default_tier_covers_no_rate() {
        assert_eq!(StakeTier::default(), StakeTier::None);
        assert!(!StakeTier::None.covers(1));
        assert!(!StakeTier::None.covers(u64::MAX));
    }

    #[test]
    fn test_tier_coverage_boundaries() {
        assert!(StakeTier::UpTo1Gbps.covers(1_000_000_000));
        assert!(!StakeTier::UpTo1Gbps.covers(1_000_000_001));
        assert!(StakeTier::UpTo5Gbps.covers(5_000_000_000));
        assert!(!StakeTier::UpTo5Gbps.covers(5_000_000_001));
        assert!(StakeTier::Unmetered.covers(u64::MAX));

        // A zero rate is not a commitment, so no tier covers it.
        for tier in [
            StakeTier::None,
            StakeTier::UpTo1Gbps,
            StakeTier::UpTo5Gbps,
            StakeTier::Unmetered,
        ] {
            assert!(!tier.covers(0), "{tier} should not cover a zero rate");
        }
    }
}
