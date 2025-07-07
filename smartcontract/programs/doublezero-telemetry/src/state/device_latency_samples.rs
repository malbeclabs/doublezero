use crate::state::accounttype::AccountType;
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;

/// Maximum number of RTT samples storable in a single account.
/// At 5-second intervals, this allows ~48 hours of data.
pub const MAX_DEVICE_LATENCY_SAMPLES: usize = 35_000;

/// Size in bytes of the fixed header structure stored at the start of the account data.
pub const DEVICE_LATENCY_SAMPLES_HEADER_SIZE: usize =
    1 + 8 + 32 + 32 + 32 + 32 + 32 + 32 + 8 + 8 + 4 + 128;

/// Total size of a fully preallocated account, including the header and all samples.
pub const DEVICE_LATENCY_SAMPLES_ALLOCATED_SIZE: usize =
    DEVICE_LATENCY_SAMPLES_HEADER_SIZE + MAX_DEVICE_LATENCY_SAMPLES * 4;

/// Fixed header structure stored at the beginning of a `DeviceLatencySamples` account.
///
/// This metadata describes the context for a series of latency measurements
/// between two devices linked over a network path.
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct DeviceLatencySamplesHeader {
    /// Account type discriminator.
    pub account_type: AccountType, // 1

    /// Epoch for which the samples were collected.
    pub epoch: u64, // 8

    /// PDA public key of the agent that submitted the measurements.
    pub origin_device_agent_pk: Pubkey, // 32

    /// PDA public key of the origin device.
    pub origin_device_pk: Pubkey, // 32

    /// PDA public key of the target device.
    pub target_device_pk: Pubkey, // 32

    /// PDA public key of the origin device location.
    pub origin_device_location_pk: Pubkey, // 32

    /// PDA public key of the target device location.
    pub target_device_location_pk: Pubkey, // 32

    /// PDA public key of the link.
    pub link_pk: Pubkey, // 32

    /// Sampling interval in microseconds.
    pub sampling_interval_microseconds: u64, // 8

    /// Start timestamp in microseconds.
    pub start_timestamp_microseconds: u64, // 8

    /// Index of the next sample to write.
    pub next_sample_index: u32, // 4

    /// Unused padding reserved for future use.
    pub _unused: [u8; 128], // 128
}

impl DeviceLatencySamplesHeader {
    /// Parses account data and returns the deserialized header and the vector of samples.
    ///
    /// Fails if the input is too short for the header or the declared number of samples.
    pub fn from_account_data(data: &[u8]) -> Result<(Self, Vec<u32>), borsh::io::Error> {
        if data.len() < DEVICE_LATENCY_SAMPLES_HEADER_SIZE {
            return Err(borsh::io::Error::new(
                std::io::ErrorKind::UnexpectedEof,
                "account data too short for header",
            ));
        }

        let (header_bytes, sample_bytes) = data.split_at(DEVICE_LATENCY_SAMPLES_HEADER_SIZE);
        let header = Self::try_from(header_bytes)?;

        if sample_bytes.len() < header.next_sample_index as usize * 4 {
            return Err(borsh::io::Error::new(
                std::io::ErrorKind::UnexpectedEof,
                "account data too short for sample count",
            ));
        }

        let samples = sample_bytes
            .chunks_exact(4)
            .take(header.next_sample_index as usize)
            .map(|chunk| u32::from_le_bytes(chunk.try_into().unwrap()))
            .collect();

        Ok((header, samples))
    }

    /// Returns the static size in bytes of the header.
    pub fn size(&self) -> usize {
        DEVICE_LATENCY_SAMPLES_HEADER_SIZE
    }
}

impl TryFrom<&[u8]> for DeviceLatencySamplesHeader {
    type Error = borsh::io::Error;

    /// Deserializes the header from a byte slice.
    ///
    /// Does not read or validate any samples following the header.
    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        DeviceLatencySamplesHeader::deserialize(&mut &data[..])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_device_latency_samples_from_header_bytes() {
        let samples = vec![100u32, 200u32, 300u32, 400u32, 500u32];
        let header = DeviceLatencySamplesHeader {
            account_type: AccountType::DeviceLatencySamples,
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
        };

        let mut data = borsh::to_vec(&header).unwrap();
        data.extend(samples.iter().flat_map(|s| s.to_le_bytes()));

        let parsed_header = DeviceLatencySamplesHeader::try_from(data.as_slice()).unwrap();

        assert_eq!(header.account_type, parsed_header.account_type);
        assert_eq!(header.epoch, parsed_header.epoch);
        assert_eq!(header.origin_device_pk, parsed_header.origin_device_pk);
        assert_eq!(header.target_device_pk, parsed_header.target_device_pk);
        assert_eq!(
            header.origin_device_location_pk,
            parsed_header.origin_device_location_pk
        );
        assert_eq!(
            header.target_device_location_pk,
            parsed_header.target_device_location_pk
        );
        assert_eq!(header.link_pk, parsed_header.link_pk);
        assert_eq!(
            header.origin_device_agent_pk,
            parsed_header.origin_device_agent_pk
        );
        assert_eq!(
            header.sampling_interval_microseconds,
            parsed_header.sampling_interval_microseconds
        );
        assert_eq!(
            header.start_timestamp_microseconds,
            parsed_header.start_timestamp_microseconds
        );
        assert_eq!(header.next_sample_index, parsed_header.next_sample_index);
    }

    #[test]
    fn test_device_latency_samples_from_account_data() {
        let samples = vec![100u32, 200u32, 300u32, 400u32, 500u32];
        let header = DeviceLatencySamplesHeader {
            account_type: AccountType::DeviceLatencySamples,
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
        };

        let mut data = borsh::to_vec(&header).unwrap();
        data.extend(samples.iter().flat_map(|s| s.to_le_bytes()));

        let (parsed_header, parsed_samples) =
            DeviceLatencySamplesHeader::from_account_data(data.as_slice()).unwrap();

        assert_eq!(header.account_type, parsed_header.account_type);
        assert_eq!(header.epoch, parsed_header.epoch);
        assert_eq!(header.origin_device_pk, parsed_header.origin_device_pk);
        assert_eq!(header.target_device_pk, parsed_header.target_device_pk);
        assert_eq!(
            header.origin_device_location_pk,
            parsed_header.origin_device_location_pk
        );
        assert_eq!(
            header.target_device_location_pk,
            parsed_header.target_device_location_pk
        );
        assert_eq!(header.link_pk, parsed_header.link_pk);
        assert_eq!(
            header.origin_device_agent_pk,
            parsed_header.origin_device_agent_pk
        );
        assert_eq!(
            header.sampling_interval_microseconds,
            parsed_header.sampling_interval_microseconds
        );
        assert_eq!(
            header.start_timestamp_microseconds,
            parsed_header.start_timestamp_microseconds
        );
        assert_eq!(header.next_sample_index, parsed_header.next_sample_index);
        assert_eq!(samples, parsed_samples);
        assert_eq!(
            data.len(),
            header.size() + samples.len() * 4,
            "Invalid Size"
        );
    }
}
