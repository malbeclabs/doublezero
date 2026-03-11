use crate::{
    seeds::SEED_TIMESTAMP_INDEX,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;
use std::{
    fmt,
    io::{self, Read, Write},
};

/// Maximum number of timestamp index entries per account.
/// At one entry per write batch, this supports well beyond a 48-hour epoch.
pub const MAX_TIMESTAMP_INDEX_ENTRIES: usize = 10_000;

/// Header size in bytes:
/// - 1 byte: account_type
/// - 32 bytes: samples_account_pk
/// - 4 bytes: next_entry_index
/// - 64 bytes: _unused (reserved)
///
/// Total: 101 bytes
pub const TIMESTAMP_INDEX_HEADER_SIZE: usize = {
    1 // account_type
    + 32 // samples_account_pk
    + 4 // next_entry_index
    + 64 // _unused
};

/// Size of a single timestamp index entry in bytes:
/// - 4 bytes: sample_index (u32)
/// - 8 bytes: timestamp_microseconds (u64)
pub const TIMESTAMP_INDEX_ENTRY_SIZE: usize = 4 + 8;

/// Onchain header for a timestamp index account.
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct TimestampIndexHeader {
    pub account_type: AccountType, // 1

    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub samples_account_pk: Pubkey, // 32

    pub next_entry_index: u32, // 4

    #[cfg_attr(feature = "serde", serde(with = "serde_bytes"))]
    pub _unused: [u8; 64], // 64
}

impl TryFrom<&[u8]> for TimestampIndexHeader {
    type Error = borsh::io::Error;

    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        if data.len() < TIMESTAMP_INDEX_HEADER_SIZE {
            return Err(borsh::io::Error::new(
                std::io::ErrorKind::UnexpectedEof,
                "account data too short for timestamp index header",
            ));
        }

        Self::deserialize(&mut &data[..])
    }
}

/// A single entry in the timestamp index.
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone, Copy)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct TimestampIndexEntry {
    pub sample_index: u32,
    pub timestamp_microseconds: u64,
}

/// Structured representation of a timestamp index account.
#[derive(Debug, PartialEq, Clone)]
pub struct TimestampIndex {
    pub header: TimestampIndexHeader,
    pub entries: Vec<TimestampIndexEntry>,
}

impl fmt::Display for TimestampIndex {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, samples_account: {}, entries: {}",
            self.header.account_type,
            self.header.samples_account_pk,
            self.entries.len()
        )
    }
}

impl BorshSerialize for TimestampIndex {
    fn serialize<W: Write>(&self, writer: &mut W) -> io::Result<()> {
        self.header.serialize(writer)?;
        for entry in &self.entries {
            writer.write_all(&entry.sample_index.to_le_bytes())?;
            writer.write_all(&entry.timestamp_microseconds.to_le_bytes())?;
        }
        Ok(())
    }
}

impl BorshDeserialize for TimestampIndex {
    fn deserialize_reader<R: Read>(reader: &mut R) -> io::Result<Self> {
        let header = TimestampIndexHeader::deserialize_reader(reader)?;

        let num_entries = header.next_entry_index as usize;
        let mut entries = Vec::with_capacity(num_entries);
        let mut sample_buf = [0u8; 4];
        let mut ts_buf = [0u8; 8];

        for _ in 0..num_entries {
            reader.read_exact(&mut sample_buf)?;
            reader.read_exact(&mut ts_buf)?;
            entries.push(TimestampIndexEntry {
                sample_index: u32::from_le_bytes(sample_buf),
                timestamp_microseconds: u64::from_le_bytes(ts_buf),
            });
        }

        Ok(TimestampIndex { header, entries })
    }
}

impl TryFrom<&[u8]> for TimestampIndex {
    type Error = borsh::io::Error;

    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        borsh::from_slice(data)
    }
}

impl AccountTypeInfo for TimestampIndex {
    fn seed(&self) -> &[u8] {
        SEED_TIMESTAMP_INDEX
    }

    fn size(&self) -> usize {
        TIMESTAMP_INDEX_HEADER_SIZE + self.entries.len() * TIMESTAMP_INDEX_ENTRY_SIZE
    }

    fn owner(&self) -> Pubkey {
        // The timestamp index doesn't have a single "owner" agent in its header,
        // but we return the samples account pk for reference.
        self.header.samples_account_pk
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_timestamp_index_serialization() {
        let entries = vec![
            TimestampIndexEntry {
                sample_index: 0,
                timestamp_microseconds: 1_700_000_000_000_000,
            },
            TimestampIndexEntry {
                sample_index: 12,
                timestamp_microseconds: 1_700_000_000_060_000,
            },
            TimestampIndexEntry {
                sample_index: 24,
                timestamp_microseconds: 1_700_000_000_120_000,
            },
        ];
        let val = TimestampIndex {
            header: TimestampIndexHeader {
                account_type: AccountType::TimestampIndex,
                samples_account_pk: Pubkey::new_unique(),
                next_entry_index: entries.len() as u32,
                _unused: [0; 64],
            },
            entries: entries.clone(),
        };
        let header = val.header.clone();

        let data = borsh::to_vec(&val).unwrap();
        let val2 = TimestampIndex::try_from_slice(&data).unwrap();
        let header2 = val2.header.clone();

        assert_eq!(header.account_type, header2.account_type);
        assert_eq!(header.samples_account_pk, header2.samples_account_pk);
        assert_eq!(header.next_entry_index, header2.next_entry_index);
        assert_eq!(val.entries, val2.entries);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid size"
        );
    }
}
