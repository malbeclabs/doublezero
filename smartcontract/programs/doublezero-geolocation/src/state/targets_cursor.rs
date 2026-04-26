//! Read-only cursor over the packed `GeolocationTarget` bytes inside a
//! `GeolocationUser` account.
//!
//! Per-call heap usage is bounded by a single `GeolocationTarget` (71 bytes)
//! regardless of `N`. This avoids the OOM that hits the default 32 KiB BPF
//! heap when borsh-deserializing a `Vec<GeolocationTarget>` past N≈250.
//! See https://github.com/malbeclabs/doublezero/issues/3591.

use crate::state::geolocation_user::GeolocationTarget;
use solana_program::program_error::ProgramError;

/// Wire size of one `GeolocationTarget` (1 + 4 + 2 + 32 + 32). Pinned by the
/// `stride_matches_wire_format` test.
pub const STRIDE: usize = 71;

/// Borrowed view over the targets payload of a `GeolocationUser` account.
/// Construct with [`TargetsCursor::new`] from the slice that *follows* the
/// `u32` length prefix.
#[derive(Debug)]
pub struct TargetsCursor<'a> {
    bytes: &'a [u8],
    count: u32,
}

impl<'a> TargetsCursor<'a> {
    /// Build a cursor over `bytes`, whose first `count * STRIDE` bytes hold
    /// `count` packed targets. Trailing bytes (e.g. `result_destination`) are
    /// ignored. Returns `InvalidAccountData` if `bytes` is too short.
    pub fn new(bytes: &'a [u8], count: u32) -> Result<Self, ProgramError> {
        let needed = (count as usize)
            .checked_mul(STRIDE)
            .ok_or(ProgramError::InvalidAccountData)?;
        if bytes.len() < needed {
            return Err(ProgramError::InvalidAccountData);
        }
        Ok(Self {
            bytes: &bytes[..needed],
            count,
        })
    }

    pub fn len(&self) -> u32 {
        self.count
    }

    pub fn is_empty(&self) -> bool {
        self.count == 0
    }

    /// Decode the target at `index`. Each call deserializes 71 bytes onto the
    /// stack — no heap allocation.
    pub fn get(&self, index: u32) -> Result<GeolocationTarget, ProgramError> {
        if index >= self.count {
            return Err(ProgramError::InvalidArgument);
        }
        let start = (index as usize) * STRIDE;
        let slice = &self.bytes[start..start + STRIDE];
        borsh::from_slice(slice).map_err(|_| ProgramError::InvalidAccountData)
    }

    /// Sequentially-decoded iterator.
    pub fn iter(&self) -> impl Iterator<Item = Result<GeolocationTarget, ProgramError>> + '_ {
        (0..self.count).map(move |i| self.get(i))
    }

    /// Index of the first target satisfying `pred`, or `None`. Stops scanning
    /// at the first match.
    pub fn find_match<F>(&self, mut pred: F) -> Result<Option<u32>, ProgramError>
    where
        F: FnMut(&GeolocationTarget) -> bool,
    {
        for i in 0..self.count {
            let t = self.get(i)?;
            if pred(&t) {
                return Ok(Some(i));
            }
        }
        Ok(None)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::geolocation_user::GeoLocationTargetType;
    use borsh::BorshSerialize;
    use solana_program::pubkey::Pubkey;
    use std::net::Ipv4Addr;

    fn sample_target(seed: u32) -> GeolocationTarget {
        let octets = seed.to_be_bytes();
        GeolocationTarget {
            target_type: GeoLocationTargetType::Outbound,
            ip_address: Ipv4Addr::new(10, octets[1], octets[2], octets[3]),
            location_offset_port: 8000 + (seed as u16 & 0x0fff),
            target_pk: Pubkey::new_from_array([seed as u8; 32]),
            geoprobe_pk: Pubkey::new_from_array([(seed as u8).wrapping_add(1); 32]),
        }
    }

    fn pack(targets: &[GeolocationTarget]) -> Vec<u8> {
        let mut out = Vec::with_capacity(targets.len() * STRIDE);
        for t in targets {
            t.serialize(&mut out).unwrap();
        }
        out
    }

    #[test]
    fn stride_matches_wire_format() {
        // The cursor's STRIDE constant is load-bearing; if a future field
        // change shifts the wire size, all the byte arithmetic in the
        // mutating helpers breaks silently. This guards against that.
        let target = sample_target(0);
        let bytes = borsh::to_vec(&target).unwrap();
        assert_eq!(bytes.len(), STRIDE);
    }

    #[test]
    fn iter_round_trips() {
        let targets: Vec<_> = (0..5).map(sample_target).collect();
        let bytes = pack(&targets);
        let cursor = TargetsCursor::new(&bytes, 5).unwrap();
        let decoded: Vec<_> = cursor.iter().map(|r| r.unwrap()).collect();
        assert_eq!(decoded, targets);
    }

    #[test]
    fn find_match_returns_index() {
        let targets: Vec<_> = (0..10).map(sample_target).collect();
        let bytes = pack(&targets);
        let cursor = TargetsCursor::new(&bytes, 10).unwrap();
        let needle = sample_target(7);
        let idx = cursor.find_match(|t| *t == needle).unwrap();
        assert_eq!(idx, Some(7));
    }

    #[test]
    fn find_match_returns_none() {
        let targets: Vec<_> = (0..3).map(sample_target).collect();
        let bytes = pack(&targets);
        let cursor = TargetsCursor::new(&bytes, 3).unwrap();
        let result = cursor.find_match(|t| t.location_offset_port == 0).unwrap();
        assert_eq!(result, None);
    }

    #[test]
    fn get_out_of_range_errors() {
        let cursor = TargetsCursor::new(&[], 0).unwrap();
        assert_eq!(
            cursor.get(0).unwrap_err(),
            ProgramError::InvalidArgument
        );
    }

    #[test]
    fn new_rejects_short_buffer() {
        let bytes = vec![0u8; STRIDE - 1];
        assert_eq!(
            TargetsCursor::new(&bytes, 1).unwrap_err(),
            ProgramError::InvalidAccountData
        );
    }

    #[test]
    fn new_ignores_trailing_bytes() {
        // Cursor only owns the targets section; trailing bytes (e.g. the
        // following `result_destination` length prefix) are correctly out
        // of scope.
        let target = sample_target(42);
        let mut bytes = pack(std::slice::from_ref(&target));
        bytes.extend_from_slice(&[0xab, 0xcd, 0xef]); // simulated trailing data
        let cursor = TargetsCursor::new(&bytes, 1).unwrap();
        assert_eq!(cursor.len(), 1);
        assert_eq!(cursor.get(0).unwrap(), target);
    }
}
