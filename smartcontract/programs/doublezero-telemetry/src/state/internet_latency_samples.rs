use crate::{
    seeds::SEED_INTERNET_LATENCY_SAMPLES,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;
use std::{
    fmt,
    io::{self, Read, Write},
};

/// Maximum number of RTT samples storable in a single account
/// With 1-minute intervals, 3_000 samples ~= 48 hours of data.
pub const MAX_INTERNET_LATENCY_SAMPLES: usize = 3_000;
pub const MAX_DATA_PROVIDER_NAME_BYTES: usize = 32;

/// Static size of the `InternetLatencySamples` struct without the `samples` vec.
/// Used to calculate initial account allocation. Bytes per field:
/// - 1 byte: `account_type`
/// - 1 byte: `bump_seed`
/// - 32 bytes: `data_provider_name`
/// - 8 byte: `epoch`
/// - 3 * 32 bytes: pubkeys for `agent`, `exchanges`
/// - 8 bytes: `sampling_interval_microseconds`
/// - 8 bytes: `start_timestamp_microseconds`
/// - 4 bytes: `next_sample_index`
/// - 128 bytes: reserved for future use
///
/// Total size: 290 bytes
pub const INTERNET_LATENCY_SAMPLES_MAX_HEADER_SIZE: usize =
    INTERNET_LATENCY_SAMPLES_HEADER_SIZE_MINUS_PROVIDER + MAX_DATA_PROVIDER_NAME_BYTES;

const INTERNET_LATENCY_SAMPLES_HEADER_SIZE_MINUS_PROVIDER: usize = {
    1 // account_type
    + 8 // epoch
    + 4 // data_provider_name.len()
    + 32 // oracle_agent_pk
    + 32 // origin_exchange_pk
    + 32 // target_exchange_pk
    + 8 // sampling_interval_microseconds
    + 8 // start_timestamp_microseconds
    + 4 // next_sample_index
    + 128 // _unused
};

/// Onchain data structure representing a latency samples account header between two
/// dz exchanges over the public internet for a specific epoch and third party probe provider,
/// written by a single agent account managed by the serviceability global state.
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct InternetLatencySamplesHeader {
    // Discriminator to distinguish from other accounts during deserialization
    pub account_type: AccountType, // 1
    // Epoch number in which samples were collected
    pub epoch: u64, // 8
    // Name of the third-party provider of the sampling probes
    pub data_provider_name: String, // 32 bytes
    // Agent authorized to write RTT samples (must match the signer)
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string"
        )
    )]
    pub oracle_agent_pk: Pubkey, // 32
    // Cached exchange of the probe origin for query/UI optimization
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string"
        )
    )]
    pub origin_exchange_pk: Pubkey, // 32
    // Cached exchange of the probe target
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string"
        )
    )]
    pub target_exchange_pk: Pubkey, // 32
    // Sampling interval configured by the agent (in microseconds)
    pub sampling_interval_microseconds: u64, // 8
    // Timestamp of the first written sample (µs since UNIX epoch)
    // Set on the first write, remains unchanged on subsequent writes
    pub start_timestamp_microseconds: u64, // 8
    // Tracks how many samples have been appended
    pub next_sample_index: u32, // 4
    // Reserved for future use
    #[cfg_attr(feature = "serde", serde(with = "serde_bytes"))]
    pub _unused: [u8; 128], // 128
}

impl InternetLatencySamplesHeader {
    pub fn data_provider_name_length(data: &[u8]) -> Result<usize, std::array::TryFromSliceError> {
        const DATA_PROVIDER_LOC: usize = 1 + 8;

        // Based on the account layout and borsh's 4-byte string field prefix
        data[DATA_PROVIDER_LOC..DATA_PROVIDER_LOC + 4]
            .try_into()
            .map(|bytes| u32::from_le_bytes(bytes) as usize)
    }

    pub fn instance_size(data_provider_name_size: usize) -> usize {
        INTERNET_LATENCY_SAMPLES_HEADER_SIZE_MINUS_PROVIDER + data_provider_name_size
    }
}

impl TryFrom<&[u8]> for InternetLatencySamplesHeader {
    type Error = borsh::io::Error;

    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        // ensures no zero-length data provider names
        if data.len() <= INTERNET_LATENCY_SAMPLES_MAX_HEADER_SIZE - MAX_DATA_PROVIDER_NAME_BYTES {
            return Err(borsh::io::Error::new(
                std::io::ErrorKind::UnexpectedEof,
                "account data too short for header",
            ));
        }

        let name_length =
            InternetLatencySamplesHeader::data_provider_name_length(data).map_err(|_| {
                borsh::io::Error::new(
                    std::io::ErrorKind::InvalidInput,
                    "account data doesn't have valid provider name prefix",
                )
            })?;
        let header_len = InternetLatencySamplesHeader::instance_size(name_length);

        if header_len > INTERNET_LATENCY_SAMPLES_MAX_HEADER_SIZE {
            return Err(borsh::io::Error::new(
                std::io::ErrorKind::UnexpectedEof,
                "account data too long for header",
            ));
        }

        Self::deserialize(&mut &data[..header_len])
    }
}

/// Structured representation of a latency samples account
///
/// This is not the onchain data structure, but a convenience wrapper for the header and samples.
#[derive(Clone, Debug, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct InternetLatencySamples {
    pub header: InternetLatencySamplesHeader,
    pub samples: Vec<u32>,
}

impl fmt::Display for InternetLatencySamples {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, data_provider_name: {}, epoch: {}, oracle_agent: {}, origin_exchange: {}, target_exchange: {}, samples: {}",
            self.header.account_type, self.header.data_provider_name, self.header.epoch, self.header.oracle_agent_pk, self.header.origin_exchange_pk, self.header.target_exchange_pk, self.samples.len(),
        )
    }
}

impl BorshSerialize for InternetLatencySamples {
    fn serialize<W: Write>(&self, writer: &mut W) -> io::Result<()> {
        self.header.serialize(writer)?;
        _ = &self
            .samples
            .iter()
            .try_for_each(|sample| writer.write_all(&sample.to_le_bytes()))?;
        Ok(())
    }
}

impl BorshDeserialize for InternetLatencySamples {
    fn deserialize_reader<R: Read>(reader: &mut R) -> io::Result<Self> {
        let header = InternetLatencySamplesHeader::deserialize_reader(reader)?;

        let num_samples = header.next_sample_index as usize;
        let mut samples = Vec::with_capacity(num_samples);
        let mut buf = [0u8; 4];

        for _ in 0..num_samples {
            reader.read_exact(&mut buf)?;
            samples.push(u32::from_le_bytes(buf));
        }

        Ok(InternetLatencySamples { header, samples })
    }
}

impl TryFrom<&[u8]> for InternetLatencySamples {
    type Error = borsh::io::Error;

    /// Enables deserialize from raw Solana account data.
    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        borsh::from_slice(data)
    }
}

impl AccountTypeInfo for InternetLatencySamples {
    /// Returns the fixed seed associated with this account type.
    fn seed(&self) -> &[u8] {
        SEED_INTERNET_LATENCY_SAMPLES
    }

    /// Computes the full serialized size of this account (for realloc).
    /// Used when dynamically resizing to accommodate more samples.
    fn size(&self) -> usize {
        INTERNET_LATENCY_SAMPLES_HEADER_SIZE_MINUS_PROVIDER
            + self.header.data_provider_name.len()
            + self.samples.len() * 4
    }

    /// Returns the public key of the agent who owns/writes to the account
    fn owner(&self) -> Pubkey {
        self.header.oracle_agent_pk
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_internet_latency_samples_serialized() {
        let samples: Vec<u32> = vec![100, 200, 300, 400, 500];
        let val = InternetLatencySamples {
            header: InternetLatencySamplesHeader {
                account_type: AccountType::InternetLatencySamples,
                data_provider_name: "RIPE Atlas".to_string(),
                epoch: 19_800,
                oracle_agent_pk: Pubkey::new_unique(),
                origin_exchange_pk: Pubkey::new_unique(),
                target_exchange_pk: Pubkey::new_unique(),
                sampling_interval_microseconds: 60_000_000,
                start_timestamp_microseconds: 1_700_000_000_000_000,
                next_sample_index: samples.len() as u32,
                _unused: [0; 128],
            },
            samples,
        };
        let header = val.header.clone();

        let data = borsh::to_vec(&val).unwrap();
        let val2 = InternetLatencySamples::try_from_slice(&data).unwrap();
        let header2 = val2.header.clone();

        assert_eq!(header.account_type, header2.account_type);
        assert_eq!(header.epoch, header2.epoch);
        assert_eq!(header.data_provider_name, header2.data_provider_name);
        assert_eq!(header.oracle_agent_pk, header2.oracle_agent_pk);
        assert_eq!(header.origin_exchange_pk, header2.origin_exchange_pk);
        assert_eq!(header.target_exchange_pk, header2.target_exchange_pk);
        assert_eq!(
            header.sampling_interval_microseconds,
            header2.sampling_interval_microseconds
        );
        assert_eq!(
            header.start_timestamp_microseconds,
            header2.start_timestamp_microseconds
        );
        assert_eq!(header.next_sample_index, header2.next_sample_index);
        assert_eq!(val.samples, val2.samples);
        assert_eq!(data.len(), val.size(), "Invalid size");
    }
}
