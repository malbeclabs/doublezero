use std::collections::HashMap;

use crate::DoubleZeroClient;
use doublezero_telemetry::state::device_latency_samples::DeviceLatencySamplesHeader;
use solana_account_decoder::UiAccountEncoding;
use solana_client::{rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig}, rpc_filter::{Memcmp, MemcmpEncodedBytes, RpcFilterType}};
use solana_sdk::{commitment_config::CommitmentConfig, pubkey::Pubkey};

#[derive(Debug, PartialEq, Clone)]
pub struct ListLatencyCommand{
    pub code: String,
};

impl ListLatencyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<HashMap<Pubkey, DeviceLatencySamplesHeader>> {

        let options = RpcProgramAccountsConfig {
            filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new(
            0,
            MemcmpEncodedBytes::Bytes(vec![3]),
        ))]),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                data_slice: None,
                commitment: Some(CommitmentConfig::confirmed()),
                min_context_slot: None,
            },
            with_context: None,
            sort_results: None,
        };

        let mut list: HashMap<Pubkey, DeviceLatencySamplesHeader> = HashMap::new();
        let accounts = client.get_client()
            .get_program_accounts_with_config(client.get_program_id(), options)?;

        for (pubkey, account) in accounts {
            assert!(account.data[0] == 3, "Invalid account type");
            list.insert(pubkey, DeviceLatencySamplesHeader::try_from(&account.data[..])?);
        }

        Ok(list)
    }
}
