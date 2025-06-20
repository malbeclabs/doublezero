use crate::{
    seeds::SEED_DZ_LATENCY_SAMPLES,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone, Serialize)]
pub struct DzLatencySamples {
    pub account_type: AccountType,           // 1
    pub epoch: u64,                          // 8
    pub device_a_pk: Pubkey,                 // 32
    pub device_z_pk: Pubkey,                 // 32
    pub location_a_pk: Pubkey,               // 32
    pub location_z_pk: Pubkey,               // 32
    pub link_pk: Pubkey,                     // 32 (all 1s for internet data)
    pub agent_pk: Pubkey,                    // 32
    pub sampling_interval_microseconds: u64, // 8
    pub start_timestamp_microseconds: u64,   // 8
    pub next_sample_index: u32,              // 4
    pub bump_seed: u8,                       // 1
    pub samples: Vec<u32>,                   // 4 + n*4 (RTT values in microseconds)
}

impl fmt::Display for DzLatencySamples {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, epoch: {}, device_a: {}, device_z: {}, link: {}, agent: {}, samples: {}",
            self.account_type, self.epoch, self.device_a_pk, self.device_z_pk, self.link_pk, self.agent_pk, self.samples.len()
        )
    }
}

impl AccountTypeInfo for DzLatencySamples {
    fn seed(&self) -> &[u8] {
        SEED_DZ_LATENCY_SAMPLES
    }

    fn size(&self) -> usize {
        1 + 8 + 32 + 32 + 32 + 32 + 32 + 32 + 8 + 8 + 4 + 1 + 4 + self.samples.len() * 4
    }

    fn bump_seed(&self) -> u8 {
        self.bump_seed
    }

    /// Owner is the agent pubkey which writes data
    fn owner(&self) -> Pubkey {
        self.agent_pk
    }
}

impl TryFrom<&AccountInfo<'_>> for DzLatencySamples {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Self::try_from_slice(&data).map_err(|e| {
            msg!("Failed to deserialize DzLatencySamples: {}", e);
            ProgramError::InvalidAccountData
        })
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
            epoch: 19800,
            device_a_pk: Pubkey::new_unique(),
            device_z_pk: Pubkey::new_unique(),
            location_a_pk: Pubkey::new_unique(),
            location_z_pk: Pubkey::new_unique(),
            link_pk: Pubkey::new_unique(),
            agent_pk: Pubkey::new_unique(),
            sampling_interval_microseconds: 5_000_000,
            start_timestamp_microseconds: 1_700_000_000_000_000,
            next_sample_index: samples.len() as u32,
            bump_seed: 255,
            samples: samples.clone(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = DzLatencySamples::try_from_slice(&data).unwrap();

        assert_eq!(val.account_type, val2.account_type);
        assert_eq!(val.epoch, val2.epoch);
        assert_eq!(val.device_a_pk, val2.device_a_pk);
        assert_eq!(val.device_z_pk, val2.device_z_pk);
        assert_eq!(val.location_a_pk, val2.location_a_pk);
        assert_eq!(val.location_z_pk, val2.location_z_pk);
        assert_eq!(val.link_pk, val2.link_pk);
        assert_eq!(val.agent_pk, val2.agent_pk);
        assert_eq!(
            val.sampling_interval_microseconds,
            val2.sampling_interval_microseconds
        );
        assert_eq!(
            val.start_timestamp_microseconds,
            val2.start_timestamp_microseconds
        );
        assert_eq!(val.next_sample_index, val2.next_sample_index);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.samples, val2.samples);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
