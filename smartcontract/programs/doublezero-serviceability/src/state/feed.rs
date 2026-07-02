use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

/// A single metro entry in a [`Feed`]: an exchange and the multicast groups joinable from it.
///
/// Borsh serializes this identically to the `(Pubkey, Vec<Pubkey>)` tuple it replaced (fields in
/// declaration order), so the on-chain byte layout is unchanged.
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Clone, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct MetroGroups {
    pub exchange: Pubkey,
    pub groups: Vec<Pubkey>,
}

/// Result of matching a device's exchange (metro) against a [`Feed`]'s metro map.
#[derive(Debug, PartialEq)]
pub enum FeedMetroMatch<'a> {
    /// The feed has no metros: it imposes no metro restriction and is reachable from any
    /// exchange (and admits any group).
    Unrestricted,
    /// The exchange is covered; these are the joinable multicast groups for it.
    Groups(&'a [Pubkey]),
    /// The feed has metros but none match the exchange.
    NotCovered,
}

/// A serviceability catalog entry: the `metro(exchange) → group-set` map for one SKU.
///
/// The pubkey of this account (`feed_key`) is the SKU identifier carried on EdgeSeat access
/// passes. `code` is the PDA seed, so it is immutable; `name` and `metros` are mutable.
/// A feed with an empty `metros` vec imposes no metro restriction (reachable from anywhere).
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
    pub reference_count: u32,      // 4 - number of access passes referencing this feed
    /// `exchange_pk → group_pks`. Empty ⇒ no metro restriction.
    pub metros: Vec<MetroGroups>,
}

impl Feed {
    /// Match `exchange` against this feed's metro map. See [`FeedMetroMatch`].
    pub fn groups_for(&self, exchange: &Pubkey) -> FeedMetroMatch<'_> {
        if self.metros.is_empty() {
            return FeedMetroMatch::Unrestricted;
        }
        match self.metros.iter().find(|m| &m.exchange == exchange) {
            Some(m) => FeedMetroMatch::Groups(&m.groups),
            None => FeedMetroMatch::NotCovered,
        }
    }
}

impl fmt::Display for Feed {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, bump_seed: {}, code: {}, name: {}, reference_count: {}, metros: {}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.code,
            self.name,
            self.reference_count,
            self.metros.len()
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
            reference_count: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            metros: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
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

    fn feed_with(metros: Vec<MetroGroups>) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            code: "shreds".to_string(),
            name: "Shreds".to_string(),
            reference_count: 0,
            metros,
        }
    }

    #[test]
    fn test_feed_serialization_roundtrip() {
        let val = feed_with(vec![MetroGroups {
            exchange: Pubkey::new_unique(),
            groups: vec![Pubkey::new_unique(), Pubkey::new_unique()],
        }]);
        let data = borsh::to_vec(&val).unwrap();
        let val2 = Feed::try_from(&data[..]).unwrap();
        val.validate().unwrap();
        val2.validate().unwrap();
        assert_eq!(val, val2);
        assert_eq!(data.len(), borsh::object_length(&val).unwrap());
    }

    #[test]
    fn test_groups_for_empty_metros_is_unrestricted() {
        let feed = feed_with(vec![]);
        assert_eq!(
            feed.groups_for(&Pubkey::new_unique()),
            FeedMetroMatch::Unrestricted
        );
    }

    #[test]
    fn test_groups_for_covered_and_not_covered() {
        let fra = Pubkey::new_unique();
        let g1 = Pubkey::new_unique();
        let g2 = Pubkey::new_unique();
        let feed = feed_with(vec![MetroGroups {
            exchange: fra,
            groups: vec![g1, g2],
        }]);

        match feed.groups_for(&fra) {
            FeedMetroMatch::Groups(groups) => assert_eq!(groups, &[g1, g2]),
            other => panic!("expected Groups, got {other:?}"),
        }
        assert_eq!(
            feed.groups_for(&Pubkey::new_unique()),
            FeedMetroMatch::NotCovered
        );
    }

    #[test]
    fn test_feed_wrong_account_type_rejected() {
        let mut val = feed_with(vec![]);
        val.account_type = AccountType::Exchange;
        let data = borsh::to_vec(&val).unwrap();
        assert!(Feed::try_from(&data[..]).is_err());
    }
}
