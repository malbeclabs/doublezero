use crate::{
    seeds::SEED_DZ_LATENCY_SAMPLES,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;
use std::fmt;

/// Maximum number of samples that can be stored in a single account
/// Calculated for ~48 hours of data with samples every 5 seconds
/// 48 hours * 60 minutes * 60 seconds / 5 seconds = 34,560 samples
pub const MAX_SAMPLES: usize = 35_000;

/// Base size of DzLatencySamples account (without samples vector)
pub const DZ_LATENCY_SAMPLES_HEADER_SIZE: usize =
    1 + 1 + 8 + 32 + 32 + 32 + 32 + 32 + 32 + 8 + 8 + 4 + 4;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct DzLatencySamples {
    // TODO(snormore): Should this be versioned?
    pub account_type: AccountType,           // 1
    pub bump_seed: u8,                       // 1
    pub epoch: u64,                          // 8
    pub origin_device_agent_pk: Pubkey,      // 32
    pub origin_device_pk: Pubkey,            // 32
    pub target_device_pk: Pubkey,            // 32
    pub origin_device_location_pk: Pubkey,   // 32
    pub target_device_location_pk: Pubkey,   // 32
    pub link_pk: Pubkey,                     // 32 (all 1s for internet data)
    pub sampling_interval_microseconds: u64, // 8
    pub start_timestamp_microseconds: u64,   // 8
    pub next_sample_index: u32,              // 4
    // TODO(snormore): Leave room for future things?
    pub samples: Vec<u32>, // 4 + n*4 (RTT values in microseconds)
}

impl fmt::Display for DzLatencySamples {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, epoch: {}, origin_device_agent: {}, origin_device: {}, target_device: {}, link: {}, samples: {}",
            self.account_type, self.epoch, self.origin_device_agent_pk, self.origin_device_pk, self.target_device_pk, self.link_pk, self.samples.len()
        )
    }
}

impl AccountTypeInfo for DzLatencySamples {
    fn seed(&self) -> &[u8] {
        SEED_DZ_LATENCY_SAMPLES
    }

    fn size(&self) -> usize {
        1 + 1 + 8 + 32 + 32 + 32 + 32 + 32 + 32 + 8 + 8 + 4 + 4 + self.samples.len() * 4
    }

    fn bump_seed(&self) -> u8 {
        self.bump_seed
    }

    /// Owner is the agent pubkey which writes data
    fn owner(&self) -> Pubkey {
        self.origin_device_agent_pk
    }
}

impl TryFrom<&[u8]> for DzLatencySamples {
    type Error = borsh::io::Error;

    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        DzLatencySamples::deserialize(&mut &data[..])
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_dz_latency_samples_serialization() {
        let samples = vec![100u32, 200u32, 300u32, 400u32, 500u32];
        let val = DzLatencySamples {
            account_type: AccountType::DzLatencySamples,
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
            samples: samples.clone(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = DzLatencySamples::try_from_slice(&data).unwrap();

        assert_eq!(val.account_type, val2.account_type);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.epoch, val2.epoch);
        assert_eq!(val.origin_device_pk, val2.origin_device_pk);
        assert_eq!(val.target_device_pk, val2.target_device_pk);
        assert_eq!(
            val.origin_device_location_pk,
            val2.origin_device_location_pk
        );
        assert_eq!(
            val.target_device_location_pk,
            val2.target_device_location_pk
        );
        assert_eq!(val.link_pk, val2.link_pk);
        assert_eq!(val.origin_device_agent_pk, val2.origin_device_agent_pk);
        assert_eq!(
            val.sampling_interval_microseconds,
            val2.sampling_interval_microseconds
        );
        assert_eq!(
            val.start_timestamp_microseconds,
            val2.start_timestamp_microseconds
        );
        assert_eq!(val.next_sample_index, val2.next_sample_index);
        assert_eq!(val.samples, val2.samples);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
