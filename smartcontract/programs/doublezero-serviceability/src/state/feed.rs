use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

/// A serviceability catalog entry: one SKU scoped to a single metro (`exchange`), holding the
/// multicast groups joinable there.
///
/// The pubkey of this account (`feed_key`) is the SKU identifier carried on EdgeSeat access passes.
/// `code` and `exchange` are the PDA seeds, so both are immutable; `name` and `groups` are mutable.
/// One `feed_key` is one feed in one metro (e.g. `hyperliquid@tokyo`); a different metro is a
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
            "account_type: {}, owner: {}, bump_seed: {}, code: {}, name: {}, exchange: {}, groups: {}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.code,
            self.name,
            self.exchange,
            self.groups.len()
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
}
