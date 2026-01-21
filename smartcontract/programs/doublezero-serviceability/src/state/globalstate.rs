use crate::{
    error::{DoubleZeroError, Validate},
    helper::deserialize_vec_with_capacity,
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct GlobalState {
    pub account_type: AccountType,         // 1
    pub bump_seed: u8,                     // 1
    pub account_index: u128,               // 16
    pub foundation_allowlist: Vec<Pubkey>, // 4 + 32 * len
    // The list of device pubkeys was migrated to the Contributor structure,
    // which has an owner and is responsible for managing devices and links.
    pub _device_allowlist: Vec<Pubkey>, // 4 + 32 * len
    // Note: This list of pubkeys is no longer used.
    // The access control logic has been migrated to AccessPasses,
    // which now act as the canonical mechanism for authorization.
    pub _user_allowlist: Vec<Pubkey>, // 4 + 32 * len
    // Authorities and settings
    pub activator_authority_pk: Pubkey,    // 32
    pub sentinel_authority_pk: Pubkey,     // 32
    pub contributor_airdrop_lamports: u64, // 8
    pub user_airdrop_lamports: u64,        // 8
    pub health_oracle_pk: Pubkey,          // 32
    pub qa_allowlist: Vec<Pubkey>,         // 4 + 32 * len
}

impl Default for GlobalState {
    fn default() -> Self {
        Self {
            account_type: AccountType::GlobalState,
            bump_seed: 0,
            account_index: 0,
            foundation_allowlist: Vec::new(),
            _device_allowlist: Vec::new(),
            _user_allowlist: Vec::new(),
            activator_authority_pk: Pubkey::default(),
            sentinel_authority_pk: Pubkey::default(),
            contributor_airdrop_lamports: 0,
            user_airdrop_lamports: 0,
            health_oracle_pk: Pubkey::default(),
            qa_allowlist: Vec::new(),
        }
    }
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
user_airdrop_lamports: {},
health_oracle_pk: {:?}",
            self.account_type,
            self.account_index,
            self.foundation_allowlist,
            self._device_allowlist,
            self._user_allowlist,
            self.activator_authority_pk,
            self.sentinel_authority_pk,
            self.contributor_airdrop_lamports,
            self.user_airdrop_lamports,
            self.health_oracle_pk,
        )
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
            _device_allowlist: deserialize_vec_with_capacity(&mut data)?,
            _user_allowlist: deserialize_vec_with_capacity(&mut data)?,
            activator_authority_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            sentinel_authority_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            contributor_airdrop_lamports: BorshDeserialize::deserialize(&mut data)
                .unwrap_or_default(),
            user_airdrop_lamports: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            health_oracle_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            qa_allowlist: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
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
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!(
                "Failed to deserialize GlobalState: {:?}",
                res.as_ref().err()
            );
        }
        res
    }
}

impl Validate for GlobalState {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::GlobalState {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }

        if self.foundation_allowlist.is_empty() {
            msg!("Foundation allowlist cannot be empty");
            return Err(DoubleZeroError::InvalidFoundationAllowlist);
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_compatibility_globalstate() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account 5cNB1387r3wt3aBAT1uoTge7y1LAadWkC9DFQ3F89Dt6 --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = ["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"];

        crate::helper::base_tests::test_parsing::<GlobalState>(&versions).unwrap();
    }

    #[test]
    fn test_state_globalstate_try_from_defaults() {
        let data = [AccountType::GlobalState as u8];
        let val = GlobalState::try_from(&data[..]).unwrap();

        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.account_index, 0);
        assert_eq!(val.foundation_allowlist, Vec::<Pubkey>::new());
        assert_eq!(val._device_allowlist, Vec::<Pubkey>::new());
        assert_eq!(val._user_allowlist, Vec::<Pubkey>::new());
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
            _device_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            _user_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            activator_authority_pk: Pubkey::new_unique(),
            sentinel_authority_pk: Pubkey::new_unique(),
            contributor_airdrop_lamports: 1_000_000_000,
            user_airdrop_lamports: 40_000,
            health_oracle_pk: Pubkey::new_unique(),
            qa_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = GlobalState::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val.account_index, val2.account_index);
        assert_eq!(val.foundation_allowlist, val2.foundation_allowlist);
        assert_eq!(val._device_allowlist, val2._device_allowlist);
        assert_eq!(val._user_allowlist, val2._user_allowlist);
        assert_eq!(val.activator_authority_pk, val2.activator_authority_pk);
        assert_eq!(val.sentinel_authority_pk, val2.sentinel_authority_pk);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
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
            _device_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            _user_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            activator_authority_pk: Pubkey::new_unique(),
            sentinel_authority_pk: Pubkey::new_unique(),
            contributor_airdrop_lamports: 1_000_000_000,
            user_airdrop_lamports: 40_000,
            health_oracle_pk: Pubkey::new_unique(),
            qa_allowlist: vec![Pubkey::new_unique(), Pubkey::new_unique()],
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }
}
