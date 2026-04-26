//! Heap-friendly read of a `GeolocationUser` account.
//!
//! `GeolocationUser::try_from` borsh-deserializes `targets: Vec<GeolocationTarget>`
//! onto the BPF heap, which OOMs at N≈250 (#3591). This view reads the same
//! wire format but **skips** past the targets bytes — their length is fully
//! determined by the preceding `u32` count, so we can record the offset and
//! continue parsing `result_destination` without materializing any per-target
//! data. Heap usage is bounded by `code` + `result_destination` lengths,
//! independent of N.
//!
//! Use `cursor()` to scan the targets section. Use `with_cursor()` for the
//! common borrow + scan + drop pattern.

use crate::state::{
    accounttype::AccountType,
    geolocation_user::{GeolocationBillingConfig, GeolocationPaymentStatus, GeolocationUserStatus},
    targets_cursor::{TargetsCursor, STRIDE},
};
use borsh::BorshDeserialize;
use solana_program::{
    account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey,
};

/// Parsed `GeolocationUser` metadata + a byte offset to the targets section.
/// All owned; the targets payload itself is left in the account buffer.
#[derive(Debug, Clone, PartialEq)]
pub struct GeolocationUserView {
    pub account_type: AccountType,
    pub owner: Pubkey,
    pub code: String,
    pub token_account: Pubkey,
    pub payment_status: GeolocationPaymentStatus,
    pub billing: GeolocationBillingConfig,
    pub status: GeolocationUserStatus,

    /// Number of targets in the targets section.
    pub targets_count: u32,
    /// Byte offset (within the account data) at which the first target begins,
    /// i.e. immediately after the `u32` length prefix.
    pub targets_offset: usize,

    pub result_destination: String,
}

impl GeolocationUserView {
    /// Parse the metadata. The targets section is *not* deserialized.
    pub fn try_from_slice(data: &[u8]) -> Result<Self, ProgramError> {
        let mut reader = data;

        let account_type = AccountType::deserialize_reader(&mut reader)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        if account_type != AccountType::GeolocationUser {
            msg!("Invalid account type: {}", account_type);
            return Err(ProgramError::InvalidAccountData);
        }

        let owner = Pubkey::deserialize_reader(&mut reader)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        let code = String::deserialize_reader(&mut reader)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        let token_account = Pubkey::deserialize_reader(&mut reader)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        let payment_status = GeolocationPaymentStatus::deserialize_reader(&mut reader)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        let billing = GeolocationBillingConfig::deserialize_reader(&mut reader)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        let status = GeolocationUserStatus::deserialize_reader(&mut reader)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        let targets_count = u32::deserialize_reader(&mut reader)
            .map_err(|_| ProgramError::InvalidAccountData)?;

        // Position right after the targets length prefix.
        let targets_offset = data.len() - reader.len();

        let targets_bytes = (targets_count as usize)
            .checked_mul(STRIDE)
            .ok_or(ProgramError::InvalidAccountData)?;
        if reader.len() < targets_bytes {
            return Err(ProgramError::InvalidAccountData);
        }
        reader = &reader[targets_bytes..];

        // `result_destination` is BDI-style trailing-grace: empty if the
        // account predates the field being added.
        let result_destination = if reader.is_empty() {
            String::new()
        } else {
            String::deserialize_reader(&mut reader)
                .map_err(|_| ProgramError::InvalidAccountData)?
        };

        Ok(Self {
            account_type,
            owner,
            code,
            token_account,
            payment_status,
            billing,
            status,
            targets_count,
            targets_offset,
            result_destination,
        })
    }

    /// Borrow the account data and parse a view from it.
    pub fn try_from_account(account: &AccountInfo) -> Result<Self, ProgramError> {
        let data = account.try_borrow_data()?;
        Self::try_from_slice(&data)
    }

    /// Build a `TargetsCursor` over the targets section of `data`.
    pub fn cursor<'a>(&self, data: &'a [u8]) -> Result<TargetsCursor<'a>, ProgramError> {
        let end = self
            .targets_offset
            .checked_add((self.targets_count as usize) * STRIDE)
            .ok_or(ProgramError::InvalidAccountData)?;
        if data.len() < end {
            return Err(ProgramError::InvalidAccountData);
        }
        TargetsCursor::new(&data[self.targets_offset..end], self.targets_count)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::geolocation_user::{
        FlatPerEpochConfig, GeoLocationTargetType, GeolocationTarget, GeolocationUser,
    };
    use std::net::Ipv4Addr;

    fn sample_target(seed: u32) -> GeolocationTarget {
        let octets = seed.to_be_bytes();
        GeolocationTarget {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(10, octets[1], octets[2], octets[3]),
            location_offset_port: 8000,
            target_pk: Pubkey::default(),
            geoprobe_pk: Pubkey::new_from_array([seed as u8; 32]),
        }
    }

    fn sample_user(targets: Vec<GeolocationTarget>, result_destination: &str) -> GeolocationUser {
        GeolocationUser {
            account_type: AccountType::GeolocationUser,
            owner: Pubkey::new_unique(),
            code: "geo-user-01".to_string(),
            token_account: Pubkey::new_unique(),
            payment_status: GeolocationPaymentStatus::Paid,
            billing: GeolocationBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
                rate: 1000,
                last_deduction_dz_epoch: 42,
            }),
            status: GeolocationUserStatus::Activated,
            targets,
            result_destination: result_destination.to_string(),
        }
    }

    fn assert_view_matches(view: &GeolocationUserView, user: &GeolocationUser) {
        assert_eq!(view.account_type, user.account_type);
        assert_eq!(view.owner, user.owner);
        assert_eq!(view.code, user.code);
        assert_eq!(view.token_account, user.token_account);
        assert_eq!(view.payment_status, user.payment_status);
        assert_eq!(view.billing, user.billing);
        assert_eq!(view.status, user.status);
        assert_eq!(view.targets_count, user.targets.len() as u32);
        assert_eq!(view.result_destination, user.result_destination);
    }

    #[test]
    fn parses_user_with_zero_targets() {
        let user = sample_user(vec![], "host:1234");
        let bytes = borsh::to_vec(&user).unwrap();
        let view = GeolocationUserView::try_from_slice(&bytes).unwrap();
        assert_view_matches(&view, &user);
        assert_eq!(view.targets_count, 0);
    }

    #[test]
    fn parses_user_with_many_targets_without_materializing_them() {
        let targets: Vec<_> = (0..1000).map(sample_target).collect();
        let user = sample_user(targets.clone(), "");
        let bytes = borsh::to_vec(&user).unwrap();
        let view = GeolocationUserView::try_from_slice(&bytes).unwrap();
        assert_view_matches(&view, &user);

        // The cursor should hand back exactly what we serialized.
        let cursor = view.cursor(&bytes).unwrap();
        for (i, original) in targets.iter().enumerate() {
            assert_eq!(&cursor.get(i as u32).unwrap(), original);
        }
    }

    #[test]
    fn targets_offset_is_correct() {
        let target = sample_target(7);
        let user = sample_user(vec![target.clone()], "");
        let bytes = borsh::to_vec(&user).unwrap();
        let view = GeolocationUserView::try_from_slice(&bytes).unwrap();

        // The bytes at `targets_offset` should round-trip through borsh as
        // the same target.
        let slice = &bytes[view.targets_offset..view.targets_offset + STRIDE];
        let decoded: GeolocationTarget = borsh::from_slice(slice).unwrap();
        assert_eq!(decoded, target);
    }

    #[test]
    fn rejects_wrong_account_type() {
        let mut bytes = borsh::to_vec(&sample_user(vec![], "")).unwrap();
        bytes[0] = AccountType::GeoProbe as u8;
        assert_eq!(
            GeolocationUserView::try_from_slice(&bytes).unwrap_err(),
            ProgramError::InvalidAccountData
        );
    }

    #[test]
    fn rejects_truncated_targets_section() {
        let user = sample_user((0..3).map(sample_target).collect(), "");
        let mut bytes = borsh::to_vec(&user).unwrap();
        // Drop a byte from inside the targets section.
        bytes.truncate(bytes.len() - STRIDE - 5);
        assert_eq!(
            GeolocationUserView::try_from_slice(&bytes).unwrap_err(),
            ProgramError::InvalidAccountData
        );
    }

    #[test]
    fn handles_missing_result_destination_field() {
        // Simulate an old account written before result_destination existed:
        // borsh-serialize the user, then strip the trailing 4-byte empty
        // String prefix.
        let user = sample_user(vec![sample_target(1)], "");
        let mut bytes = borsh::to_vec(&user).unwrap();
        bytes.truncate(bytes.len() - 4);
        let view = GeolocationUserView::try_from_slice(&bytes).unwrap();
        assert_eq!(view.targets_count, 1);
        assert_eq!(view.result_destination, "");
    }
}
