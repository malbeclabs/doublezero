use crate::{
    error::{DoubleZeroError, Validate},
    helper::deserialize_vec_with_capacity,
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
pub struct GlobalState {
    pub account_type: AccountType,         // 1
    pub bump_seed: u8,                     // 1
    pub account_index: u128,               // 16
    pub foundation_allowlist: Vec<Pubkey>, // 4 + 32 * len
    pub device_allowlist: Vec<Pubkey>,     // 4 + 32 * len
    pub user_allowlist: Vec<Pubkey>,       // 4 + 32 * len
    pub activator_authority_pk: Pubkey,    // 32
    pub sentinel_authority_pk: Pubkey,     // 32
    pub contributor_airdrop_lamports: u64, // 8
    pub user_airdrop_lamports: u64,        // 8
}

impl fmt::Display for GlobalState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, \
account_index: {}, \
foundation_allowlist: {:?}, \
device_allowlist: {:?}, \
user_allowlist: {:?}, \
activator_authority_pk: {:?}, \
sentinel_authority_pk: {:?}, \
contributor_airdrop_lamports: {}, \
user_airdrop_lamports: {}",
            self.account_type,
            self.account_index,
            self.foundation_allowlist,
            self.device_allowlist,
            self.user_allowlist,
            self.activator_authority_pk,
            self.sentinel_authority_pk,
            self.contributor_airdrop_lamports,
            self.user_airdrop_lamports,
        )
    }
}

impl GlobalState {
    pub fn size(&self) -> usize {
        1 + 1
            + 16
            + 4
            + (self.foundation_allowlist.len() * 32)
            + 4
            + (self.device_allowlist.len() * 32)
            + 4
            + (self.user_allowlist.len() * 32)
            + 32
            + 32
            + 8
            + 8
    }
}

impl TryFrom<&[u8]> for GlobalState {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            account_index: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            foundation_allowlist: deserialize_vec_with_capacity(&mut data)?,
            device_allowlist: deserialize_vec_with_capacity(&mut data)?,
            user_allowlist: deserialize_vec_with_capacity(&mut data)?,
            activator_authority_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            sentinel_authority_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            contributor_airdrop_lamports: BorshDeserialize::deserialize(&mut data)
                .unwrap_or_default(),
            user_airdrop_lamports: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::GlobalState {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for GlobalState {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Self::try_from(&data[..])
    }
}

impl Validate for GlobalState {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::GlobalState {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_globalstate_try_from_defaults() {
        let data = [AccountType::GlobalState as u8];
        let val = GlobalState::try_from(&data[..]).unwrap();

        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.account_index, 0);
        assert_eq!(val.foundation_allowlist, Vec::<Pubkey>::new());
        assert_eq!(val.device_allowlist, Vec::<Pubkey>::new());
        assert_eq!(val.user_allowlist, Vec::<Pubkey>::new());
        assert_eq!(val.activator_authority_pk, Pubkey::default());
        assert_eq!(val.sentinel_authority_pk, Pubkey::default());
        assert_eq!(val.contributor_airdrop_lamports, 0);
        assert_eq!(val.user_airdrop_lamports, 0);
    }

    #[test]
    fn test_state_globalstate_serialization() {
        let val = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed: 1,
            account_index: 123,
            foundation_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            device_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            user_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            activator_authority_pk: Pubkey::new_unique(),
            sentinel_authority_pk: Pubkey::new_unique(),
            contributor_airdrop_lamports: 1_000_000_000,
            user_airdrop_lamports: 40_000,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = GlobalState::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.account_index, val2.account_index);
        assert_eq!(val.foundation_allowlist, val2.foundation_allowlist);
        assert_eq!(val.device_allowlist, val2.device_allowlist);
        assert_eq!(val.user_allowlist, val2.user_allowlist);
        assert_eq!(val.activator_authority_pk, val2.activator_authority_pk);
        assert_eq!(val.sentinel_authority_pk, val2.sentinel_authority_pk);
        assert_eq!(data.len(), val.size(), "Invalid Size");
        assert_eq!(
            val.contributor_airdrop_lamports,
            val2.contributor_airdrop_lamports
        );
        assert_eq!(val.user_airdrop_lamports, val2.user_airdrop_lamports);
    }

    #[test]
    fn test_state_globalstate_validate_error_invalid_account_type() {
        let val = GlobalState {
            account_type: AccountType::Device, // Should be GlobalState
            bump_seed: 1,
            account_index: 123,
            foundation_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            device_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            user_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            activator_authority_pk: Pubkey::new_unique(),
            sentinel_authority_pk: Pubkey::new_unique(),
            contributor_airdrop_lamports: 1_000_000_000,
            user_airdrop_lamports: 40_000,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }
}
