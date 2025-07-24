use crate::{
    seeds::SEED_INET_LATENCY_SAMPLES,
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
pub const MAX_INTERNET_SAMPLES: usize = 3_000;

/// Static size of the `InternetLatencySamples` struct without the `samples` vec.
/// Used to calculate initial account allocation. Bytes per field:
/// - 1 byte: `account_type`
/// - 1 byte: `bump_seed`
/// - 32 bytes: `data_provider_name`
/// - 8 byte: `epoch`
/// - 3 * 32 bytes: pubkeys for `agent`, `locations`
/// - 8 bytes: `start_timestamp_microseconds`
/// - 4 bytes: `next_sample_index`
/// - 128 bytes: reserved for future use
///
/// Total size: 281 bytes
pub const INTERNET_LATENCY_SAMPLES_HEADER_SIZE: usize = 1 + 1 + 32 + 8 + 32 + 32 + 32 + 8 + 4 + 128;

/// Onchain data structure representing a latency samples account header between two
/// location over the public internet for a specific epoch and third party probe provider,
/// written by a single agent account managed by the serviceability global state.
#[derive(BorshDeserialize, BorshSerialize, Debug, PartialEq, Clone)]
pub struct InternetLatencySamplesHeader {
    // Discriminator to distinguish from other accounts during deserialization
    pub account_type: AccountType, // 1
    // Required for deriving and recreating the PDA
    pub bump_seed: u8, // 1
    // Name of the third-party provider of the sampling probes
    pub data_provider_name: String, // 32 bytes
    // Epoch number in which samples were collected
    pub epoch: u64, // 8
    // Agent authorized to write RTT samples (must match the signer)
    pub oracle_agent_pk: Pubkey, // 32
    // Cached location of the probe origin for query/UI optimization
    pub origin_location_pk: Pubkey, // 32
    // Cached location of the probe target
    pub target_location_pk: Pubkey, // 32
    // Timestamp of the first written sample (µs since UNIX epoch)
    // Set on the first write, remains unchanged on subsequent writes
    pub start_timestamp_microseconds: u64, // 8
    // Tracks how many samples have been appended
    pub next_samples_index: u32, // 4
    // Reserved for future use
    pub _unused: [u8; 128], // 128
}

impl TryFrom<&[u8]> for InternetLatencySamplesHeader {
    type Error = borsh::io::Error;

    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        if data.len() < INTERNET_LATENCY_SAMPLES_HEADER_SIZE {
            return Err(borsh::io::Error::new(
                std::io::ErrorKind::UnexpectedEof,
                "account data too short for header",
            ));
        }

        Self::deserialize(&mut &data[..])
    }
}

/// Structured representation of a latency samples account
///
/// This is not the onchain data structure, but a convenience wrapper for the header and samples.
#[derive(Clone, Debug, PartialEq)]
pub struct InternetLatencySamples {
    pub header: InternetLatencySamplesHeader,
    pub samples: Vec<u32>,
}

impl fmt::Display for InternetLatencySamples {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, data_provider_name: {}, epoch: {}, oracle_agent: {}, origin_location: {}, target_location: {}, samples: {}",
            self.header.account_type, self.header.data_provider_name, self.header.epoch, self.header.oracle_agent_pk, self.header.origin_location_pk, self.header.target_location_pk, self.samples.len(),
        )
    }
}

impl BorshSerialize for InternetLatencySamples {
    fn serialize<W: Write>(&self, writer: &mut W) -> io::Result<()> {
        self.header.serialize(writer)?;
        &self
            .samples
            .iter()
            .for_each(|sample| writer.write_all(&sample.to_le_bytes())?);
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
        SEED_INET_LATENCY_SAMPLES
    }

    /// Computes the full serialized size of this account (for realloc).
    /// Used when dynamically resizing to accommodate more samples.
    fn size(&self) -> usize {
        INTERNET_LATENCY_SAMPLES_HEADER_SIZE + self.samples.len() * 4
    }

    /// Returns the bump seed used during PDA derivation
    fn bump_seed(&self) -> u8 {
        self.header.bump_seed
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
        let samples = vec![100u32, 200u32, 300u32, 400u32, 500u32];
        let val = InternetLatencySamples {
            header: InternetLatencySamplesHeader {
                account_type: AccountType::InternetLatencySamples,
                bump_seed: 255,
                data_provider_name: "RIPE Atlas".to_string(),
                epoch: 19_800,
                oracle_agent_pk: Pubkey::new_unique(),
                origin_location_pk: Pubkey::new_unique(),
                target_location_pk: Pubkey::new_unique(),
                start_timestamp_microseconds: 1_700_000_000_000_000,
                next_samples_index: samples.len() as u32,
                _unused: [0; 128],
            },
            samples,
        };
        let header = val.header.clone();

        let data = borsh::to_vec(&val).unwrap();
        let val2 = InternetLatencySamples::try_from_slice(&data).unwrap();
        let header2 = val2.header.clone();

        assert_eq!(header.account_type, header2.account_type);
        assert_eq!(header.bump_seed, header2.bump_seed);
        assert_eq!(header.epoch, header2.epoch);
        assert_eq!(header.data_provider_name, header2.data_provider_name);
        assert_eq!(header.oracle_agent_pk, header2.oracle_agent_pk);
        assert_eq!(header.origin_location_pk, header2.origin_location_pk);
        assert_eq!(header.target_location_pk, header2.target_location_pk);
        assert_eq!(
            header.start_timestamp_microseconds,
            header2.start_timestamp_microseconds
        );
        assert_eq!(header.next_samples_index, header2.next_samples_index);
        assert_eq!(val.samples, val2.samples);
        assert_eq!(data.len(), val.size(), "Invalid size");
    }
}
