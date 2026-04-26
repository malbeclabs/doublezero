//! Read-only cursor over the packed `GeolocationTarget` bytes inside a
//! `GeolocationUser` account.
//!
//! Per-call heap usage is bounded by a single `GeolocationTarget` (71 bytes)
//! regardless of `N`. This avoids the OOM that hits the default 32 KiB BPF
//! heap when borsh-deserializing a `Vec<GeolocationTarget>` past N≈250.

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

/// Remove the target at `index` via swap-remove: copies the bytes of the
/// last target over the slot at `index`, then shifts trailing bytes (e.g.
/// `result_destination`) left by `STRIDE`. Patches the `u32` count at
/// `targets_offset - 4`. Returns the new count.
///
/// Caller must subsequently resize the account by `-STRIDE` to drop the
/// stale trailing bytes left at the end of the buffer.
pub fn swap_remove_target_bytes(
    data: &mut [u8],
    targets_offset: usize,
    targets_count: u32,
    index: u32,
) -> Result<u32, ProgramError> {
    if index >= targets_count {
        return Err(ProgramError::InvalidArgument);
    }
    let count_offset = targets_offset
        .checked_sub(4)
        .ok_or(ProgramError::InvalidAccountData)?;
    let new_count = targets_count - 1;
    let removed_offset = targets_offset + (index as usize) * STRIDE;
    let last_offset = targets_offset + (new_count as usize) * STRIDE;

    // 1. If we're removing a non-last entry, copy the last target's bytes
    // into the removed slot. (For index == new_count the slot is already at
    // the boundary; no swap needed.)
    if index != new_count {
        data.copy_within(last_offset..last_offset + STRIDE, removed_offset);
    }

    // 2. Shift trailing bytes left by STRIDE so they sit immediately after
    // the (now smaller) targets section.
    let trailing_src = last_offset + STRIDE;
    let trailing_len = data.len().saturating_sub(trailing_src);
    if trailing_len > 0 {
        data.copy_within(trailing_src..trailing_src + trailing_len, last_offset);
    }

    // 3. Patch the count prefix.
    data[count_offset..count_offset + 4].copy_from_slice(&new_count.to_le_bytes());

    Ok(new_count)
}

/// Insert `target_bytes` at the end of the targets section in `data`, shifting
/// any trailing bytes (e.g. `result_destination`) by `+STRIDE`. The buffer
/// must already be sized to the post-append length (i.e. caller has resized
/// the account by `+STRIDE` first). Patches the `u32` count at
/// `targets_offset - 4`. Returns the new count.
pub fn append_target_bytes(
    data: &mut [u8],
    targets_offset: usize,
    targets_count: u32,
    target_bytes: &[u8; STRIDE],
) -> Result<u32, ProgramError> {
    let count_offset = targets_offset
        .checked_sub(4)
        .ok_or(ProgramError::InvalidAccountData)?;
    let new_count = targets_count
        .checked_add(1)
        .ok_or(ProgramError::InvalidAccountData)?;
    let insert_at = targets_offset
        .checked_add((targets_count as usize) * STRIDE)
        .ok_or(ProgramError::InvalidAccountData)?;
    let new_len = data.len();
    let old_len = new_len
        .checked_sub(STRIDE)
        .ok_or(ProgramError::InvalidAccountData)?;
    let trailing_len = old_len
        .checked_sub(insert_at)
        .ok_or(ProgramError::InvalidAccountData)?;

    if trailing_len > 0 {
        data.copy_within(insert_at..insert_at + trailing_len, insert_at + STRIDE);
    }
    data[insert_at..insert_at + STRIDE].copy_from_slice(target_bytes);
    data[count_offset..count_offset + 4].copy_from_slice(&new_count.to_le_bytes());

    Ok(new_count)
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
        assert_eq!(cursor.get(0).unwrap_err(), ProgramError::InvalidArgument);
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

    /// Build a buffer mimicking the targets-and-trailing layout:
    /// `[count: u32 LE][targets_count * STRIDE bytes][trailing bytes]`.
    /// `targets_offset` is 4 (the count prefix is the first thing). Returns
    /// the buffer and the targets_offset.
    fn synthetic_buffer(targets: &[GeolocationTarget], trailing: &[u8]) -> (Vec<u8>, usize) {
        let mut buf = Vec::new();
        buf.extend_from_slice(&(targets.len() as u32).to_le_bytes());
        buf.extend_from_slice(&pack(targets));
        buf.extend_from_slice(trailing);
        let targets_offset = 4;
        (buf, targets_offset)
    }

    fn target_bytes(t: &GeolocationTarget) -> [u8; STRIDE] {
        let mut buf = [0u8; STRIDE];
        let mut out: &mut [u8] = &mut buf;
        t.serialize(&mut out).unwrap();
        buf
    }

    #[test]
    fn append_bytes_into_empty_targets_no_trailing() {
        let (mut data, targets_offset) = synthetic_buffer(&[], &[]);
        let new_target = sample_target(7);
        // Caller must resize first.
        data.resize(data.len() + STRIDE, 0);

        let new_count =
            append_target_bytes(&mut data, targets_offset, 0, &target_bytes(&new_target)).unwrap();

        assert_eq!(new_count, 1);
        // Verify the count prefix updated.
        assert_eq!(u32::from_le_bytes(data[0..4].try_into().unwrap()), 1);
        // Verify the bytes round-trip.
        let cursor = TargetsCursor::new(&data[targets_offset..targets_offset + STRIDE], 1).unwrap();
        assert_eq!(cursor.get(0).unwrap(), new_target);
    }

    #[test]
    fn append_bytes_with_trailing_shifts_correctly() {
        let existing: Vec<_> = (0..3).map(sample_target).collect();
        let trailing = b"\x05\x00\x00\x00hello"; // mimics result_destination = "hello"
        let (mut data, targets_offset) = synthetic_buffer(&existing, trailing);
        let new_target = sample_target(99);
        let pre_len = data.len();
        data.resize(pre_len + STRIDE, 0);

        let new_count = append_target_bytes(
            &mut data,
            targets_offset,
            existing.len() as u32,
            &target_bytes(&new_target),
        )
        .unwrap();

        assert_eq!(new_count, (existing.len() + 1) as u32);
        // Existing targets unchanged.
        let cursor = TargetsCursor::new(
            &data[targets_offset..targets_offset + new_count as usize * STRIDE],
            new_count,
        )
        .unwrap();
        for (i, original) in existing.iter().enumerate() {
            assert_eq!(&cursor.get(i as u32).unwrap(), original);
        }
        // New target at the end.
        assert_eq!(cursor.get(existing.len() as u32).unwrap(), new_target);
        // Trailing bytes intact at the new offset.
        let trailing_offset = targets_offset + new_count as usize * STRIDE;
        assert_eq!(
            &data[trailing_offset..trailing_offset + trailing.len()],
            trailing
        );
    }

    #[test]
    fn append_bytes_round_trip_oracle() {
        // Oracle: keep a Vec<GeolocationTarget> mirror; after each append,
        // the borsh-serialized Vec must match the cursor view of `data`.
        let mut oracle: Vec<GeolocationTarget> = Vec::new();
        let trailing = b"\x00\x00\x00\x00"; // empty result_destination
        let (mut data, targets_offset) = synthetic_buffer(&oracle, trailing);

        for i in 0..50 {
            let new = sample_target(1000 + i);
            data.resize(data.len() + STRIDE, 0);
            let new_count = append_target_bytes(
                &mut data,
                targets_offset,
                oracle.len() as u32,
                &target_bytes(&new),
            )
            .unwrap();
            oracle.push(new);
            assert_eq!(new_count, oracle.len() as u32);

            // Every existing target should still be readable.
            let cursor = TargetsCursor::new(
                &data[targets_offset..targets_offset + new_count as usize * STRIDE],
                new_count,
            )
            .unwrap();
            for (j, expected) in oracle.iter().enumerate() {
                assert_eq!(&cursor.get(j as u32).unwrap(), expected);
            }
            // Trailing bytes intact.
            let trailing_offset = targets_offset + new_count as usize * STRIDE;
            assert_eq!(
                &data[trailing_offset..trailing_offset + trailing.len()],
                trailing
            );
        }
    }

    #[test]
    fn swap_remove_bytes_last_index_no_swap() {
        let existing: Vec<_> = (0..3).map(sample_target).collect();
        let trailing = b"\x05\x00\x00\x00hello";
        let (mut data, targets_offset) = synthetic_buffer(&existing, trailing);

        let new_count =
            swap_remove_target_bytes(&mut data, targets_offset, existing.len() as u32, 2).unwrap();
        assert_eq!(new_count, 2);

        let cursor = TargetsCursor::new(
            &data[targets_offset..targets_offset + new_count as usize * STRIDE],
            new_count,
        )
        .unwrap();
        assert_eq!(cursor.get(0).unwrap(), existing[0]);
        assert_eq!(cursor.get(1).unwrap(), existing[1]);

        // Trailing bytes should now sit immediately after the truncated
        // targets section — i.e. at `targets_offset + 2 * STRIDE`.
        let trailing_offset = targets_offset + 2 * STRIDE;
        assert_eq!(
            &data[trailing_offset..trailing_offset + trailing.len()],
            trailing
        );
    }

    #[test]
    fn swap_remove_bytes_middle_index_swaps_last_in() {
        let existing: Vec<_> = (0..5).map(sample_target).collect();
        let (mut data, targets_offset) = synthetic_buffer(&existing, &[]);

        let new_count =
            swap_remove_target_bytes(&mut data, targets_offset, existing.len() as u32, 1).unwrap();
        assert_eq!(new_count, 4);

        let cursor = TargetsCursor::new(
            &data[targets_offset..targets_offset + new_count as usize * STRIDE],
            new_count,
        )
        .unwrap();
        // index 0 unchanged
        assert_eq!(cursor.get(0).unwrap(), existing[0]);
        // index 1 now holds what used to be the last entry (existing[4])
        assert_eq!(cursor.get(1).unwrap(), existing[4]);
        // index 2,3 unchanged
        assert_eq!(cursor.get(2).unwrap(), existing[2]);
        assert_eq!(cursor.get(3).unwrap(), existing[3]);
    }

    #[test]
    fn swap_remove_bytes_out_of_range_errors() {
        let existing = vec![sample_target(0)];
        let (mut data, targets_offset) = synthetic_buffer(&existing, &[]);
        assert_eq!(
            swap_remove_target_bytes(&mut data, targets_offset, 1, 1).unwrap_err(),
            ProgramError::InvalidArgument
        );
    }

    #[test]
    fn append_and_swap_remove_oracle() {
        // Drive the byte helpers through an interleaved sequence of appends
        // and removes; mirror the same sequence on a Vec<GeolocationTarget>;
        // after each step assert the cursor view matches the oracle.
        let trailing = b"\x05\x00\x00\x00hello";
        let mut oracle: Vec<GeolocationTarget> = Vec::new();
        let (mut data, targets_offset) = synthetic_buffer(&oracle, trailing);

        // Operations: A=append, R=remove. Indices interpreted against the
        // oracle's current length, with `% len()`.
        let ops = [
            ("A", 0),
            ("A", 1),
            ("A", 2),
            ("A", 3),
            ("A", 4),
            ("R", 1), // remove middle
            ("R", 2),
            ("A", 99),
            ("R", 0), // remove first
            ("A", 100),
            ("A", 101),
            ("R", 4), // remove last (oracle len after last A is 5)
        ];

        for (op, key) in ops {
            match op {
                "A" => {
                    let new = sample_target(2000 + key);
                    data.resize(data.len() + STRIDE, 0);
                    let new_count = append_target_bytes(
                        &mut data,
                        targets_offset,
                        oracle.len() as u32,
                        &target_bytes(&new),
                    )
                    .unwrap();
                    oracle.push(new);
                    assert_eq!(new_count, oracle.len() as u32);
                }
                "R" => {
                    let idx = (key as usize) % oracle.len();
                    let new_count = swap_remove_target_bytes(
                        &mut data,
                        targets_offset,
                        oracle.len() as u32,
                        idx as u32,
                    )
                    .unwrap();
                    oracle.swap_remove(idx);
                    let new_size = data.len() - STRIDE;
                    data.truncate(new_size);
                    assert_eq!(new_count, oracle.len() as u32);
                }
                _ => unreachable!(),
            }

            // After every op, cursor view must match the oracle exactly.
            let cursor = TargetsCursor::new(
                &data[targets_offset..targets_offset + oracle.len() * STRIDE],
                oracle.len() as u32,
            )
            .unwrap();
            for (i, expected) in oracle.iter().enumerate() {
                assert_eq!(&cursor.get(i as u32).unwrap(), expected);
            }
            // Trailing bytes must always sit at the end, intact.
            let trailing_off = targets_offset + oracle.len() * STRIDE;
            assert_eq!(&data[trailing_off..trailing_off + trailing.len()], trailing);
        }
    }
}
