use crate::{
    seeds::SEED_THIRDPARTY_LATENCY_SAMPLES,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use serde::Serialize;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone, Serialize)]
pub struct ThirdPartyLatencySamples {
    pub account_type: AccountType,         // 1
    pub data_provider_name: [u8; 32],      // 32
    pub epoch: u64,                        // 8
    pub location_a_pk: Pubkey,             // 32
    pub location_z_pk: Pubkey,             // 32
    pub start_timestamp_microseconds: u64, // 8
    pub next_sample_index: u32,            // 4
    pub bump_seed: u8,                     // 1
    pub agent_pk: Pubkey,                  // 32 - The agent authorized to write to this account
    pub samples: Vec<u32>,                 // 4 + n*4 (RTT values in microseconds)
}

impl fmt::Display for ThirdPartyLatencySamples {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let provider_str = String::from_utf8_lossy(&self.data_provider_name)
            .trim_end_matches('\0')
            .to_string();
        write!(
            f,
            "account_type: {}, provider: {}, epoch: {}, location_a: {}, location_z: {}, samples: {}",
            self.account_type, provider_str, self.epoch, self.location_a_pk, self.location_z_pk, self.samples.len()
        )
    }
}

impl AccountTypeInfo for ThirdPartyLatencySamples {
    fn seed(&self) -> &[u8] {
        SEED_THIRDPARTY_LATENCY_SAMPLES
    }

    fn size(&self) -> usize {
        1 + 32 + 8 + 32 + 32 + 8 + 4 + 1 + 32 + 4 + self.samples.len() * 4
    }

    fn bump_seed(&self) -> u8 {
        self.bump_seed
    }

    /// Owner is the agent pubkey which writes data
    fn owner(&self) -> Pubkey {
        self.agent_pk
    }
}

impl TryFrom<&AccountInfo<'_>> for ThirdPartyLatencySamples {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Self::try_from_slice(&data).map_err(|e| {
            msg!("Failed to deserialize ThirdPartyLatencySamples: {}", e);
            ProgramError::InvalidAccountData
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_thirdparty_latency_samples_serialization() {
        let mut provider_name = [0u8; 32];
        provider_name[..9].copy_from_slice(b"WonderNet");

        let samples = vec![100u32, 200u32, 300u32, 400u32, 500u32];
        let agent_pk = Pubkey::new_unique();
        let val = ThirdPartyLatencySamples {
            account_type: AccountType::ThirdPartyLatencySamples,
            data_provider_name: provider_name,
            epoch: 19800,
            location_a_pk: Pubkey::new_unique(),
            location_z_pk: Pubkey::new_unique(),
            start_timestamp_microseconds: 1_700_000_000_000_000,
            next_sample_index: samples.len() as u32,
            bump_seed: 255,
            agent_pk,
            samples: samples.clone(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = ThirdPartyLatencySamples::try_from_slice(&data).unwrap();

        assert_eq!(val.account_type, val2.account_type);
        assert_eq!(val.data_provider_name, val2.data_provider_name);
        assert_eq!(val.epoch, val2.epoch);
        assert_eq!(val.location_a_pk, val2.location_a_pk);
        assert_eq!(val.location_z_pk, val2.location_z_pk);
        assert_eq!(
            val.start_timestamp_microseconds,
            val2.start_timestamp_microseconds
        );
        assert_eq!(val.next_sample_index, val2.next_sample_index);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.agent_pk, val2.agent_pk);
        assert_eq!(val.samples, val2.samples);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
