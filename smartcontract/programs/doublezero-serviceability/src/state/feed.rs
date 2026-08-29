use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

/// A serviceability catalog entry: one feed scoped to a single metro (`exchange`), holding the
/// multicast groups joinable there.
///
/// The pubkey of this account is the `feed_key` carried on EdgeSeat access passes.
/// `code` and `exchange` are the PDA seeds, so both are immutable; `name`, `groups` and
/// `permissionless` are mutable. One `feed_key` is one feed in one metro (e.g. `shreds@tokyo`);
/// a different metro is a different feed account. Every account sharing a `code` is one SKU
/// (malbeclabs/infra#2390) — a readability term for the storefront, not something stored here.
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
    /// Whether this feed is offered without an access grant. Declarative: no instruction reads
    /// it, and the paid gate is still the EdgeSeat FeedSeat. It is a catalog label the storefront
    /// reads back from `feed list`, so `false` on an account written before this field existed is
    /// the correct answer, not a missing one.
    pub permissionless: bool, // 1
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
            "account_type: {}, owner: {}, bump_seed: {}, code: {}, name: {}, exchange: {}, groups: {}, permissionless: {}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.code,
            self.name,
            self.exchange,
            self.groups.len(),
            self.permissionless
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
            // EOF on an account written before this field existed, which reads false.
            permissionless: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
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
            permissionless: false,
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

    #[test]
    fn test_feed_wrong_account_type_rejected() {
        let mut val = feed_with(Pubkey::new_unique(), vec![Pubkey::new_unique()]);
        val.account_type = AccountType::Exchange;
        let data = borsh::to_vec(&val).unwrap();
        assert!(Feed::try_from(&data[..]).is_err());
    }

    /// A Feed written before `permissionless` existed still decodes, reading false — the
    /// per-field `unwrap_or_default()` in `TryFrom<&[u8]>` is what makes the 151 accounts
    /// already on mainnet need no migration.
    #[test]
    fn test_feed_backward_compat_no_permissionless() {
        #[derive(BorshSerialize)]
        struct LegacyFeed {
            account_type: AccountType,
            owner: Pubkey,
            bump_seed: u8,
            code: String,
            name: String,
            exchange: Pubkey,
            groups: Vec<Pubkey>,
        }

        let exchange = Pubkey::new_unique();
        let group = Pubkey::new_unique();
        let legacy = LegacyFeed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 7,
            code: "shreds".to_string(),
            name: "Shreds".to_string(),
            exchange,
            groups: vec![group],
        };

        let bytes = borsh::to_vec(&legacy).unwrap();
        let decoded = Feed::try_from(&bytes[..]).unwrap();

        assert!(!decoded.permissionless);
        // The fields before it still land where they should: a decoder that mis-read the
        // missing byte would corrupt these rather than only the flag.
        assert_eq!(decoded.code, "shreds");
        assert_eq!(decoded.exchange, exchange);
        assert_eq!(decoded.groups, vec![group]);
    }

    /// The flag round-trips when it is set, so the trailing byte is really written and read
    /// rather than always defaulting.
    #[test]
    fn test_feed_permissionless_round_trip() {
        let mut val = feed_with(Pubkey::new_unique(), vec![Pubkey::new_unique()]);
        val.permissionless = true;
        let data = borsh::to_vec(&val).unwrap();
        assert!(Feed::try_from(&data[..]).unwrap().permissionless);
    }
}
