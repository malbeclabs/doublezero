use crate::{
    error::{GeolocationError, Validate},
    state::accounttype::AccountType,
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, net::Ipv4Addr};

pub const MAX_TARGETS: usize = 4096;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum PaymentStatus {
    #[default]
    Unpaid = 0,
    Paid = 1,
}

impl TryFrom<u8> for PaymentStatus {
    type Error = ProgramError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            0 => Ok(PaymentStatus::Unpaid),
            1 => Ok(PaymentStatus::Paid),
            _ => Err(ProgramError::InvalidInstructionData),
        }
    }
}

impl fmt::Display for PaymentStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            PaymentStatus::Unpaid => write!(f, "unpaid"),
            PaymentStatus::Paid => write!(f, "paid"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum GeolocationUserStatus {
    #[default]
    Active = 0,
    Suspended = 1,
}

impl TryFrom<u8> for GeolocationUserStatus {
    type Error = ProgramError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            0 => Ok(GeolocationUserStatus::Active),
            1 => Ok(GeolocationUserStatus::Suspended),
            _ => Err(ProgramError::InvalidInstructionData),
        }
    }
}

impl fmt::Display for GeolocationUserStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            GeolocationUserStatus::Active => write!(f, "active"),
            GeolocationUserStatus::Suspended => write!(f, "suspended"),
        }
    }
}

#[derive(BorshSerialize, BorshDeserialize, Debug, Clone, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct GeolocationTarget {
    pub target_ip: Ipv4Addr, // 4
    pub target_port: u16,    // 2
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub probe_pk: Pubkey, // 32
}

impl fmt::Display for GeolocationTarget {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "target_ip: {}, target_port: {}, probe_pk: {}",
            self.target_ip, self.target_port, self.probe_pk,
        )
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct GeolocationUser {
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
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub token_account: Pubkey, // 32
    pub payment_status: PaymentStatus, // 1
    pub billing: u64,              // 8
    pub status: GeolocationUserStatus, // 1
    pub targets: Vec<GeolocationTarget>, // 4 + ~38 * len
}

impl fmt::Display for GeolocationUser {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, bump_seed: {}, code: {}, token_account: {}, \
            payment_status: {}, billing: {}, status: {}, targets: {:?}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.code,
            self.token_account,
            self.payment_status,
            self.billing,
            self.status,
            self.targets,
        )
    }
}

impl TryFrom<&[u8]> for GeolocationUser {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            code: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            token_account: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            payment_status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            billing: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            targets: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::GeolocationUser {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for GeolocationUser {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!(
                "Failed to deserialize GeolocationUser: {:?}",
                res.as_ref().err()
            );
        }
        res
    }
}

impl Validate for GeolocationUser {
    fn validate(&self) -> Result<(), GeolocationError> {
        if self.account_type != AccountType::GeolocationUser {
            msg!("Invalid account type: {}", self.account_type);
            return Err(GeolocationError::InvalidAccountType);
        }
        if self.code.len() > 32 {
            msg!("Code too long: {} bytes", self.code.len());
            return Err(GeolocationError::InvalidCodeLength);
        }
        if self.targets.len() > MAX_TARGETS {
            msg!(
                "Too many targets: {} (max {})",
                self.targets.len(),
                MAX_TARGETS
            );
            return Err(GeolocationError::MaxTargetsReached);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_geolocation_user_try_from_defaults() {
        let data = [AccountType::GeolocationUser as u8];
        let val = GeolocationUser::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.code, "");
        assert_eq!(val.token_account, Pubkey::default());
        assert_eq!(val.payment_status, PaymentStatus::Unpaid);
        assert_eq!(val.billing, 0);
        assert_eq!(val.status, GeolocationUserStatus::Active);
        assert_eq!(val.targets.len(), 0);
    }

    #[test]
    fn test_state_geolocation_user_serialization() {
        let val = GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            code: "geo-user-01".to_string(),
            token_account: Pubkey::new_unique(),
            payment_status: PaymentStatus::Paid,
            billing: 1_000_000,
            status: GeolocationUserStatus::Active,
            targets: vec![
                GeolocationTarget {
                    target_ip: [8, 8, 8, 8].into(),
                    target_port: 443,
                    probe_pk: Pubkey::new_unique(),
                },
                GeolocationTarget {
                    target_ip: [1, 1, 1, 1].into(),
                    target_port: 80,
                    probe_pk: Pubkey::new_unique(),
                },
            ],
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = GeolocationUser::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.code, val2.code);
        assert_eq!(val.token_account, val2.token_account);
        assert_eq!(val.payment_status, val2.payment_status);
        assert_eq!(val.billing, val2.billing);
        assert_eq!(val.status, val2.status);
        assert_eq!(val.targets, val2.targets);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_geolocation_user_validate_error_invalid_account_type() {
        let val = GeolocationUser {
            account_type: AccountType::GeoProbe, // Should be GeolocationUser
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            code: "geo-user-01".to_string(),
            token_account: Pubkey::new_unique(),
            payment_status: PaymentStatus::Paid,
            billing: 1_000_000,
            status: GeolocationUserStatus::Active,
            targets: vec![],
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), GeolocationError::InvalidAccountType);
    }

    #[test]
    fn test_state_geolocation_user_validate_error_code_too_long() {
        let val = GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            code: "a".repeat(33), // More than 32 bytes
            token_account: Pubkey::new_unique(),
            payment_status: PaymentStatus::Unpaid,
            billing: 0,
            status: GeolocationUserStatus::Active,
            targets: vec![],
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), GeolocationError::InvalidCodeLength);
    }

    #[test]
    fn test_state_geolocation_user_validate_error_too_many_targets() {
        let targets: Vec<GeolocationTarget> = (0..MAX_TARGETS + 1)
            .map(|i| GeolocationTarget {
                target_ip: Ipv4Addr::new(8, 8, (i >> 8) as u8, i as u8),
                target_port: 443,
                probe_pk: Pubkey::new_unique(),
            })
            .collect();

        let val = GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            code: "geo-user-01".to_string(),
            token_account: Pubkey::new_unique(),
            payment_status: PaymentStatus::Paid,
            billing: 1_000_000,
            status: GeolocationUserStatus::Active,
            targets,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), GeolocationError::MaxTargetsReached);
    }

    #[test]
    fn test_state_geolocation_user_try_from_invalid_account_type() {
        let data = [AccountType::None as u8];
        let result = GeolocationUser::try_from(&data[..]);
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), ProgramError::InvalidAccountData);
    }

    #[test]
    fn test_payment_status_try_from_u8() {
        assert_eq!(PaymentStatus::try_from(0u8).unwrap(), PaymentStatus::Unpaid);
        assert_eq!(PaymentStatus::try_from(1u8).unwrap(), PaymentStatus::Paid);
        assert_eq!(
            PaymentStatus::try_from(255u8).unwrap_err(),
            ProgramError::InvalidInstructionData
        );
    }

    #[test]
    fn test_geolocation_user_status_try_from_u8() {
        assert_eq!(
            GeolocationUserStatus::try_from(0u8).unwrap(),
            GeolocationUserStatus::Active
        );
        assert_eq!(
            GeolocationUserStatus::try_from(1u8).unwrap(),
            GeolocationUserStatus::Suspended
        );
        assert_eq!(
            GeolocationUserStatus::try_from(255u8).unwrap_err(),
            ProgramError::InvalidInstructionData
        );
    }

    #[test]
    fn test_payment_status_display() {
        assert_eq!(PaymentStatus::Unpaid.to_string(), "unpaid");
        assert_eq!(PaymentStatus::Paid.to_string(), "paid");
    }

    #[test]
    fn test_geolocation_user_status_display() {
        assert_eq!(GeolocationUserStatus::Active.to_string(), "active");
        assert_eq!(GeolocationUserStatus::Suspended.to_string(), "suspended");
    }
}
