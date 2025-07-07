use crate::{
    seeds::SEED_DZ_LATENCY_SAMPLES,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;
use std::{
    fmt,
    io::{self, Read, Write},
};

/// Maximum number of RTT samples storable in a single account.
/// With 5-second intervals, 35,000 samples ~= 48 hours of data.
pub const MAX_SAMPLES: usize = 35_000;

/// Static size of the `DeviceLatencySamples` struct without the `samples` vector.
/// Used to calculate initial account allocation. Bytes per field:
/// - 1 byte: `account_type`
/// - 1 byte: `bump_seed`
/// - 8 bytes: `epoch`
/// - 6 * 32 bytes: pubkeys for agent, devices, locations, and link
/// - 8 bytes: `sampling_interval_microseconds`
/// - 8 bytes: `start_timestamp_microseconds`
/// - 4 bytes: `next_sample_index`
/// - 128 bytes: reserved for future use
///
/// Total size: 350 bytes
pub const DEVICE_LATENCY_SAMPLES_HEADER_SIZE: usize =
    1 + 1 + 8 + 32 + 32 + 32 + 32 + 32 + 32 + 8 + 8 + 4 + 128;

/// Onchain data structure representing a latency samples account header between two devices
/// over a link for a specific epoch, written by a single authorized agent.
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct DeviceLatencySamplesHeader {
    // Used to distinguish this account type during deserialization
    pub account_type: AccountType, // 1

    // Required for recreating the PDA (seed authority)
    pub bump_seed: u8, // 1

    // Epoch number in which samples were collected
    pub epoch: u64, // 8

    // Agent authorized to write RTT samples (must match signer)
    pub origin_device_agent_pk: Pubkey, // 32

    // Device initiating sampling
    pub origin_device_pk: Pubkey, // 32

    // Destination device in RTT path
    pub target_device_pk: Pubkey, // 32

    // Cached location of origin device for query/UI optimization
    pub origin_device_location_pk: Pubkey, // 32

    // Cached location of target device
    pub target_device_location_pk: Pubkey, // 32

    // Link over which the RTT samples were taken
    pub link_pk: Pubkey, // 32

    // Sampling interval configured by the agent (in microseconds)
    pub sampling_interval_microseconds: u64, // 8

    // Timestamp of the first written sample (µs since UNIX epoch).
    // Set on the first write, remains unchanged after.
    pub start_timestamp_microseconds: u64, // 8

    // Tracks how many samples have been appended.
    pub next_sample_index: u32, // 4

    // Reserved for future use.
    pub _unused: [u8; 128], // 128
}

impl TryFrom<&[u8]> for DeviceLatencySamplesHeader {
    type Error = borsh::io::Error;

    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        if data.len() < DEVICE_LATENCY_SAMPLES_HEADER_SIZE {
            return Err(borsh::io::Error::new(
                std::io::ErrorKind::UnexpectedEof,
                "account data too short for header",
            ));
        }

        Ok(Self::deserialize(&mut &data[..])?)
    }
}

/// Structured representation of a latency samples account.
///
/// This is not the onchain data structure, but a convenience wrapper for the header and samples.
#[derive(Debug, PartialEq, Clone)]
pub struct DeviceLatencySamples {
    pub header: DeviceLatencySamplesHeader,
    pub samples: Vec<u32>,
}

impl fmt::Display for DeviceLatencySamples {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, epoch: {}, origin_device_agent: {}, origin_device: {}, target_device: {}, link: {}, samples: {}",
            self.header.account_type, self.header.epoch, self.header.origin_device_agent_pk, self.header.origin_device_pk, self.header.target_device_pk, self.header.link_pk, self.samples.len()
        )
    }
}

impl BorshSerialize for DeviceLatencySamples {
    fn serialize<W: Write>(&self, writer: &mut W) -> io::Result<()> {
        self.header.serialize(writer)?;
        for sample in &self.samples {
            writer.write_all(&sample.to_le_bytes())?;
        }
        Ok(())
    }
}

impl BorshDeserialize for DeviceLatencySamples {
    fn deserialize_reader<R: Read>(reader: &mut R) -> io::Result<Self> {
        let header = DeviceLatencySamplesHeader::deserialize_reader(reader)?;

        let num_samples = header.next_sample_index as usize;
        let mut samples = Vec::with_capacity(num_samples);
        let mut buf = [0u8; 4];

        for _ in 0..num_samples {
            reader.read_exact(&mut buf)?;
            samples.push(u32::from_le_bytes(buf));
        }

        Ok(DeviceLatencySamples { header, samples })
    }
}

impl TryFrom<&[u8]> for DeviceLatencySamples {
    type Error = borsh::io::Error;

    /// Enables deserializing from raw Solana account data.
    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        borsh::from_slice(data)
    }
}

impl AccountTypeInfo for DeviceLatencySamples {
    /// Returns the fixed seed associated with this account type.
    fn seed(&self) -> &[u8] {
        SEED_DZ_LATENCY_SAMPLES
    }

    /// Computes the full serialized size of this account (for realloc).
    /// Used when dynamically resizing to accommodate more samples.
    fn size(&self) -> usize {
        DEVICE_LATENCY_SAMPLES_HEADER_SIZE + self.samples.len() * 4
    }

    /// Returns the bump seed used during PDA derivation.
    fn bump_seed(&self) -> u8 {
        self.header.bump_seed
    }

    /// Returns the public key of the agent who owns/writes to this account.
    fn owner(&self) -> Pubkey {
        self.header.origin_device_agent_pk
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_device_latency_samples_serialization() {
        let samples = vec![100u32, 200u32, 300u32, 400u32, 500u32];
        let val = DeviceLatencySamples {
            header: DeviceLatencySamplesHeader {
                account_type: AccountType::DeviceLatencySamples,
                bump_seed: 255,
                epoch: 19800,
                origin_device_agent_pk: Pubkey::new_unique(),
                origin_device_pk: Pubkey::new_unique(),
                target_device_pk: Pubkey::new_unique(),
                origin_device_location_pk: Pubkey::new_unique(),
                target_device_location_pk: Pubkey::new_unique(),
                link_pk: Pubkey::new_unique(),
                sampling_interval_microseconds: 5_000_000,
                start_timestamp_microseconds: 1_700_000_000_000_000,
                next_sample_index: samples.len() as u32,
                _unused: [0; 128],
            },
            samples: samples.clone(),
        };
        let header = val.header.clone();

        let header_bytes = borsh::to_vec(&val.header).unwrap();
        println!("Serialized header: {:?}", header_bytes);
        let data = borsh::to_vec(&val).unwrap();
        println!("Serialized data: {:?}", data);
        let val2 = DeviceLatencySamples::try_from_slice(&data).unwrap();
        let header2 = val2.header.clone();

        assert_eq!(header.account_type, header2.account_type);
        assert_eq!(header.bump_seed, header2.bump_seed);
        assert_eq!(header.epoch, header2.epoch);
        assert_eq!(header.origin_device_pk, header2.origin_device_pk);
        assert_eq!(header.target_device_pk, header2.target_device_pk);
        assert_eq!(
            header.origin_device_location_pk,
            header2.origin_device_location_pk
        );
        assert_eq!(
            header.target_device_location_pk,
            header2.target_device_location_pk
        );
        assert_eq!(header.link_pk, header2.link_pk);
        assert_eq!(
            header.origin_device_agent_pk,
            header2.origin_device_agent_pk
        );
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
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
