use crate::{
    error::{DoubleZeroError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct Tenant {
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
    pub code: String,              // 4 + len
    pub vrf_id: u16,               // 2
    pub reference_count: u32,      // 4
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkeylist_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkeylist_from_string"
        )
    )]
    pub administrators: Vec<Pubkey>, // 4 + (32 * len)
    pub payment_status: u8,        // 1 byte — 0=Unknown, 1=Paid, 2=Delinquent, 3=Suspended
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub token_account: Pubkey, // 32 bytes — Solana 2Z token account to monitor
}

impl fmt::Display for Tenant {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, bump_seed: {}, code: {}, vrf_id: {}, administrators: {:?}, payment_status: {}, token_account: {}",
            self.account_type, self.owner, self.bump_seed, self.code, self.vrf_id, self.administrators, self.payment_status, self.token_account
        )
    }
}

impl TryFrom<&[u8]> for Tenant {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            code: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            vrf_id: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            reference_count: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            administrators: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            payment_status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            token_account: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::Tenant {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for Tenant {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize Tenant: {:?}", res.as_ref().err());
        }
        res
    }
}

impl Validate for Tenant {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be Tenant
        if self.account_type != AccountType::Tenant {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        // Code must be less than or equal to 32 bytes
        if self.code.len() > 32 {
            msg!("Invalid code length: {}", self.code.len());
            return Err(DoubleZeroError::CodeTooLong);
        }
        // Payment status must be in range 0-3
        if self.payment_status > 3 {
            msg!("Invalid payment status: {}", self.payment_status);
            return Err(DoubleZeroError::InvalidPaymentStatus);
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_tenant_try_from_defaults() {
        let data = [AccountType::Tenant as u8];
        let val = Tenant::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.code, "");
        assert_eq!(val.vrf_id, 0);
        assert_eq!(val.reference_count, 0);
        assert_eq!(val.administrators, Vec::<Pubkey>::new());
        assert_eq!(val.payment_status, 0);
        assert_eq!(val.token_account, Pubkey::default());
    }

    #[test]
    fn test_state_tenant_serialization() {
        let val = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::default(),
            bump_seed: 1,
            reference_count: 0,
            code: "test".to_string(),
            vrf_id: 100,
            administrators: vec![Pubkey::default()],
            payment_status: 1,
            token_account: Pubkey::default(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = Tenant::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.vrf_id, val2.vrf_id);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.account_type, val2.account_type);
        assert_eq!(val.administrators, val2.administrators);
        assert_eq!(val.payment_status, val2.payment_status);
        assert_eq!(val.token_account, val2.token_account);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_tenant_validate_error_invalid_account_type() {
        let val = Tenant {
            account_type: AccountType::Device, // Should be Tenant
            owner: Pubkey::default(),
            bump_seed: 1,
            reference_count: 0,
            code: "test".to_string(),
            vrf_id: 100,
            administrators: vec![],
            payment_status: 0,
            token_account: Pubkey::default(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_tenant_validate_error_code_too_long() {
        let val = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::default(),
            bump_seed: 1,
            reference_count: 0,
            code: "a".repeat(33), // More than 32
            vrf_id: 100,
            administrators: vec![],
            payment_status: 0,
            token_account: Pubkey::default(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::CodeTooLong);
    }
}
