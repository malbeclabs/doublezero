use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

/// A bond tier from RFC-28. The bond is sized against the rate a builder commits to, so the
/// tier is what a bond buys: the highest rate a feed backed by it may commit to.
#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Eq, Default, Copy, Clone)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum BondTier {
    /// No bond. Covers no rate, which is what an account that was never written must read as.
    #[default]
    None = 0,
    UpTo1Gbps = 1,
    UpTo5Gbps = 2,
    Unmetered = 3,
}

impl BondTier {
    /// The highest committed rate this tier covers, in bits per second. Gbps is decimal here, as
    /// it is everywhere a link rate is quoted.
    pub fn max_rate_bits_per_sec(self) -> u64 {
        match self {
            BondTier::None => 0,
            BondTier::UpTo1Gbps => 1_000_000_000,
            BondTier::UpTo5Gbps => 5_000_000_000,
            BondTier::Unmetered => u64::MAX,
        }
    }

    /// Whether a feed committing to `rate_bits_per_sec` is covered by this tier. A zero rate is
    /// not a commitment, so no tier covers it.
    pub fn covers(self, rate_bits_per_sec: u64) -> bool {
        rate_bits_per_sec != 0 && rate_bits_per_sec <= self.max_rate_bits_per_sec()
    }
}

impl fmt::Display for BondTier {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            BondTier::None => "none",
            BondTier::UpTo1Gbps => "up-to-1gbps",
            BondTier::UpTo5Gbps => "up-to-5gbps",
            BondTier::Unmetered => "unmetered",
        };
        write!(f, "{s}")
    }
}

/// A builder's Solana bond, copied onto the DZ ledger.
///
/// A DZ ledger program cannot read a Solana account, so a relayer watches `builder-bond` on
/// Solana and writes what it saw here. This is an assertion, not a proof: it is worth exactly as
/// much as the key that signed it, which is recorded in `relayer`. A relayer can lie in both
/// directions, so who is allowed to sign one is a trust decision, not an implementation detail.
///
/// One mirror per bond. `bond_ref` is the PDA seed, so it is immutable. RFC-28 collateralizes
/// each feed on its own bond, so the mirror is keyed on the bond and not on the builder, and
/// `feed_key` records the one feed that bond has been spent on.
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone, Default)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct BondMirror {
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
    /// The `builder-bond` account on Solana this mirrors, and the PDA seed.
    pub bond_ref: Pubkey, // 32
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    /// The builder that posted the bond.
    pub builder: Pubkey, // 32
    /// The tier the bond bought. This is what a feed's committed rate is checked against.
    pub tier: BondTier, // 1
    /// The rate the builder declared when it posted the bond, which is what the bond was sized
    /// against. Kept for the record; the coverage check reads `tier`.
    pub committed_rate_bits_per_sec: u64, // 8
    /// The Solana slot of the `builder-bond` event this reflects. A write carrying a slot no newer
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
    /// key should not trust the bond.
    pub relayer: Pubkey, // 32
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    /// The feed this bond has been spent on, default until a feed claims it. `CreateFeed` sets it
    /// and refuses a bond that already carries one, which is what makes RFC-28's one feed per
    /// bond true. A relayer updating the mirror must carry this value forward, not zero it.
    pub feed_key: Pubkey, // 32
    /// How many multicast groups this bond has created. `CreateMulticastGroup` refuses a bond
    /// at `MAX_GROUPS_PER_BOND`, because each group takes an address from a pool every feed
    /// shares. It never goes down: deleting a group does not give the bond another one. A relayer
    /// updating the mirror must carry it forward, or every write resets the limit.
    pub groups_created: u8, // 1
}

impl fmt::Display for BondMirror {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, bump_seed: {}, bond_ref: {}, builder: {}, tier: {}, committed_rate_bits_per_sec: {}, source_slot: {}, relayer: {}, feed_key: {}, groups_created: {}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.bond_ref,
            self.builder,
            self.tier,
            self.committed_rate_bits_per_sec,
            self.source_slot,
            self.relayer,
            self.feed_key,
            self.groups_created
        )
    }
}

impl TryFrom<&[u8]> for BondMirror {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bond_ref: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            builder: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            tier: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            committed_rate_bits_per_sec: BorshDeserialize::deserialize(&mut data)
                .unwrap_or_default(),
            source_slot: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            relayer: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            feed_key: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            groups_created: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::BondMirror {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for BondMirror {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize BondMirror: {:?}", res.as_ref().err());
        }
        res
    }
}

impl Validate for BondMirror {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::BondMirror {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        if self.bond_ref == Pubkey::default() {
            msg!("BondMirror must name the bond it mirrors");
            return Err(DoubleZeroError::InvalidArgument);
        }
        if self.builder == Pubkey::default() {
            msg!("BondMirror must name a builder");
            return Err(DoubleZeroError::InvalidArgument);
        }
        if self.relayer == Pubkey::default() {
            msg!("BondMirror must record the relayer that asserted it");
            return Err(DoubleZeroError::InvalidArgument);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn mirror(tier: BondTier) -> BondMirror {
        BondMirror {
            account_type: AccountType::BondMirror,
            owner: Pubkey::new_unique(),
            bump_seed: 254,
            bond_ref: Pubkey::new_unique(),
            builder: Pubkey::new_unique(),
            tier,
            committed_rate_bits_per_sec: 1_000_000_000,
            source_slot: 123_456,
            relayer: Pubkey::new_unique(),
            feed_key: Pubkey::default(),
            groups_created: 0,
        }
    }

    #[test]
    fn test_bond_mirror_serialization_roundtrip() {
        let val = mirror(BondTier::UpTo5Gbps);
        let data = borsh::to_vec(&val).unwrap();
        let val2 = BondMirror::try_from(&data[..]).unwrap();
        val.validate().unwrap();
        val2.validate().unwrap();
        assert_eq!(val, val2);
        assert_eq!(data.len(), borsh::object_length(&val).unwrap());
    }

    #[test]
    fn test_bond_mirror_wrong_account_type_rejected() {
        let mut val = mirror(BondTier::UpTo1Gbps);
        val.account_type = AccountType::Feed;
        let data = borsh::to_vec(&val).unwrap();
        assert_eq!(
            BondMirror::try_from(&data[..]).unwrap_err(),
            ProgramError::InvalidAccountData
        );
    }

    /// Every key on a mirror identifies something a reader has to be able to check: the bond it
    /// came from, whose bond it is, and who vouched for it. A default in any of the three is an
    /// unfinished write, not a valid mirror.
    #[test]
    fn test_bond_mirror_requires_bond_builder_and_relayer() {
        for zero in [
            |m: &mut BondMirror| m.bond_ref = Pubkey::default(),
            |m: &mut BondMirror| m.builder = Pubkey::default(),
            |m: &mut BondMirror| m.relayer = Pubkey::default(),
        ] {
            let mut val = mirror(BondTier::UpTo1Gbps);
            zero(&mut val);
            assert_eq!(val.validate(), Err(DoubleZeroError::InvalidArgument));
        }
    }

    /// A fresh mirror backs no feed. `CreateFeed` reads this to decide whether the bond is spent.
    #[test]
    fn test_bond_mirror_starts_unclaimed() {
        let val = mirror(BondTier::UpTo1Gbps);
        val.validate().unwrap();
        assert_eq!(val.feed_key, Pubkey::default());
    }

    /// An account that was never written decodes as tier None, which covers nothing. A missing
    /// mirror and an empty one therefore reach create-feed as the same answer.
    #[test]
    fn test_default_tier_covers_no_rate() {
        assert_eq!(BondTier::default(), BondTier::None);
        assert!(!BondTier::None.covers(1));
        assert!(!BondTier::None.covers(u64::MAX));
    }

    #[test]
    fn test_tier_coverage_boundaries() {
        assert!(BondTier::UpTo1Gbps.covers(1_000_000_000));
        assert!(!BondTier::UpTo1Gbps.covers(1_000_000_001));
        assert!(BondTier::UpTo5Gbps.covers(5_000_000_000));
        assert!(!BondTier::UpTo5Gbps.covers(5_000_000_001));
        assert!(BondTier::Unmetered.covers(u64::MAX));

        // A zero rate is not a commitment, so no tier covers it.
        for tier in [
            BondTier::None,
            BondTier::UpTo1Gbps,
            BondTier::UpTo5Gbps,
            BondTier::Unmetered,
        ] {
            assert!(!tier.covers(0), "{tier} should not cover a zero rate");
        }
    }
}
