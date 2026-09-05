use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

/// Where a feed sits in the RFC-28 deployment lifecycle.
///
/// A feed created without a stake is `Active` on creation: the pre-RFC-28 catalog feeds have no
/// builder to attest and sell seats today. A staked feed starts `Pending` and an attestor verdict
/// moves it to `Active`.
#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Eq, Clone, Copy, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum FeedStatus {
    /// Staked and created, waiting on a conformance verdict. No seats sell.
    #[default]
    Pending = 0,
    /// Publishing and sellable.
    Active = 1,
    /// Publication stopped by the builder. Resumable.
    Halted = 2,
    /// Terminal. Set after the thirty-day notice elapses.
    Retired = 3,
}

impl fmt::Display for FeedStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let s = match self {
            FeedStatus::Pending => "pending",
            FeedStatus::Active => "active",
            FeedStatus::Halted => "halted",
            FeedStatus::Retired => "retired",
        };
        write!(f, "{s}")
    }
}

/// A serviceability catalog entry: one SKU scoped to a single metro (`exchange`), holding the
/// multicast groups joinable there.
///
/// The pubkey of this account (`feed_key`) is the SKU identifier carried on EdgeSeat access passes.
/// `code` and `exchange` are the PDA seeds, so both are immutable; `name` and `groups` are mutable.
/// One `feed_key` is one feed in one metro (e.g. `shreds@tokyo`); a different metro is a
/// different feed account.
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone, Default)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Feed {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub bump_seed: u8,             // 1
    pub code: String,              // 4 + len (PDA seed, immutable)
    pub name: String,              // 4 + len
    pub exchange: Pubkey,          // 32 (PDA seed, immutable) - the metro this feed serves
    pub groups: Vec<Pubkey>,       // 4 + 32*len - multicast groups joinable in this metro

    // RFC-28 fields. Everything below is absent from feeds created before RFC-28, so every one of
    // them decodes to a default on a short account and is written back on the next update.
    /// The builder that deployed this feed and posted its stake, zero for a catalog feed with no
    /// builder. This is the key the `StakeMirror` is written against.
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub builder: Pubkey, // 32
    /// The `BuilderStake` PDA on Solana holding this feed's deposit. A record, not a check: the DZ
    /// ledger cannot read a Solana account, so the covering check runs against `StakeMirror`.
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub stake_ref: Pubkey, // 32
    /// The `edge-feed-spec` wire format this feed conforms to, as `<spec>@<version>`
    /// (e.g. `top-of-book@v1.0.0`). A string, not an enum: specs are added in `edge-feed-spec`
    /// without a program upgrade.
    pub spec_id: String, // 4 + len
    /// SHA-256 of the service level the builder declared at deployment. The declaration lives
    /// offchain; slashing measures against it, so the hash pins which text was declared.
    pub sla_hash: [u8; 32], // 32
    /// The rate this feed committed to, in bits per second. `u64::MAX` is the unmetered tier.
    /// Not basis points: `bps` means basis points elsewhere in DoubleZero.
    pub committed_rate_bits_per_sec: u64, // 8
    pub status: FeedStatus, // 1
}

impl Feed {
    /// The multicast groups joinable when connecting from `exchange`. A feed serves exactly one
    /// metro, so this is its group set when the exchange matches, and empty otherwise.
    pub fn groups_for(&self, exchange: &Pubkey) -> &[Pubkey] {
        if &self.exchange == exchange {
            &self.groups
        } else {
            &[]
        }
    }
}

impl fmt::Display for Feed {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, bump_seed: {}, code: {}, name: {}, exchange: {}, groups: {}, builder: {}, spec_id: {}, committed_rate_bits_per_sec: {}, status: {}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.code,
            self.name,
            self.exchange,
            self.groups.len(),
            self.builder,
            self.spec_id,
            self.committed_rate_bits_per_sec,
            self.status
        )
    }
}

impl TryFrom<&[u8]> for Feed {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            code: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            name: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            exchange: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            groups: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            builder: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            stake_ref: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            spec_id: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            sla_hash: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            committed_rate_bits_per_sec: BorshDeserialize::deserialize(&mut data)
                .unwrap_or_default(),
            // Not `Pending`: a feed account written before RFC-28 has no status byte, and reading
            // one as Pending would pull every live catalog feed out of service.
            status: BorshDeserialize::deserialize(&mut data).unwrap_or(FeedStatus::Active),
        };

        if out.account_type != AccountType::Feed {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for Feed {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize Feed: {:?}", res.as_ref().err());
        }
        res
    }
}

impl Validate for Feed {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::Feed {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn feed_with(exchange: Pubkey, groups: Vec<Pubkey>) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            code: "shreds".to_string(),
            name: "Shreds".to_string(),
            exchange,
            groups,
            ..Default::default()
        }
    }

    #[test]
    fn test_feed_serialization_roundtrip() {
        let val = feed_with(
            Pubkey::new_unique(),
            vec![Pubkey::new_unique(), Pubkey::new_unique()],
        );
        let data = borsh::to_vec(&val).unwrap();
        let val2 = Feed::try_from(&data[..]).unwrap();
        val.validate().unwrap();
        val2.validate().unwrap();
        assert_eq!(val, val2);
        assert_eq!(data.len(), borsh::object_length(&val).unwrap());
    }

    #[test]
    fn test_groups_for_matching_and_other_exchange() {
        let fra = Pubkey::new_unique();
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();
        let feed = feed_with(fra, vec![g1, g2]);

        assert_eq!(feed.groups_for(&fra), &[g1, g2]);
        assert_eq!(feed.groups_for(&Pubkey::new_unique()), &[] as &[Pubkey]);
    }

    /// A feed account written before RFC-28 has no tail. It must still decode, and it must decode
    /// as Active: reading it as Pending would pull every live catalog feed out of service.
    #[test]
    fn test_pre_rfc28_feed_decodes_active_with_defaults() {
        let exchange = Pubkey::new_unique();
        let group = Pubkey::new_unique();
        let mut pre_rfc28 = Vec::new();
        AccountType::Feed.serialize(&mut pre_rfc28).unwrap();
        Pubkey::new_unique().serialize(&mut pre_rfc28).unwrap();
        1u8.serialize(&mut pre_rfc28).unwrap();
        "shreds".to_string().serialize(&mut pre_rfc28).unwrap();
        "Shreds".to_string().serialize(&mut pre_rfc28).unwrap();
        exchange.serialize(&mut pre_rfc28).unwrap();
        vec![group].serialize(&mut pre_rfc28).unwrap();

        let feed = Feed::try_from(&pre_rfc28[..]).unwrap();
        assert_eq!(feed.groups_for(&exchange), &[group]);
        assert_eq!(feed.status, FeedStatus::Active);
        assert_eq!(feed.builder, Pubkey::default());
        assert_eq!(feed.stake_ref, Pubkey::default());
        assert_eq!(feed.spec_id, "");
        assert_eq!(feed.sla_hash, [0u8; 32]);
        assert_eq!(feed.committed_rate_bits_per_sec, 0);
    }

    /// The RFC-28 tail round-trips, and a staked feed keeps the Pending it was created with.
    #[test]
    fn test_staked_feed_roundtrip_keeps_pending() {
        let mut val = feed_with(Pubkey::new_unique(), vec![Pubkey::new_unique()]);
        val.builder = Pubkey::new_unique();
        val.stake_ref = Pubkey::new_unique();
        val.spec_id = "top-of-book@v1.0.0".to_string();
        val.sla_hash = [7u8; 32];
        val.committed_rate_bits_per_sec = 1_000_000_000;
        val.status = FeedStatus::Pending;

        let data = borsh::to_vec(&val).unwrap();
        assert_eq!(Feed::try_from(&data[..]).unwrap(), val);
        assert_eq!(data.len(), borsh::object_length(&val).unwrap());
    }

    #[test]
    fn test_feed_wrong_account_type_rejected() {
        let mut val = feed_with(Pubkey::new_unique(), vec![Pubkey::new_unique()]);
        val.account_type = AccountType::Exchange;
        let data = borsh::to_vec(&val).unwrap();
        assert!(Feed::try_from(&data[..]).is_err());
    }
}
