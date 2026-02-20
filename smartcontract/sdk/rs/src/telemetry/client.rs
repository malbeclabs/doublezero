use crate::DoubleZeroClient;
use doublezero_telemetry::state::device_latency_samples::DeviceLatencySamples;
use solana_account_decoder::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, MemcmpEncodedBytes, RpcFilterType},
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

// Fetch all device latency samples for a specific epoch in a single RPC call
pub fn get_all_device_latency_samples(
    client: &dyn DoubleZeroClient,
    telemetry_program_id: &Pubkey,
    epoch: u64,
) -> eyre::Result<HashMap<Pubkey, DeviceLatencySamples>> {
    const DEVICE_LATENCY_SAMPLES_ACCOUNT_TYPE: u8 = 3;

    // Filter for DeviceLatencySamples account type and specific epoch
    let filters = vec![
        RpcFilterType::Memcmp(Memcmp::new(
            0, // account_type is the first byte
            MemcmpEncodedBytes::Bytes(vec![DEVICE_LATENCY_SAMPLES_ACCOUNT_TYPE]),
        )),
        RpcFilterType::Memcmp(Memcmp::new(
            1, // epoch starts at byte 1
            MemcmpEncodedBytes::Bytes(epoch.to_le_bytes().to_vec()),
        )),
    ];

    let options = RpcProgramAccountsConfig {
        filters: Some(filters),
        account_config: RpcAccountInfoConfig {
            encoding: Some(UiAccountEncoding::Base64),
            data_slice: None,
            commitment: Some(CommitmentConfig::confirmed()),
            min_context_slot: None,
        },
        with_context: None,
        sort_results: None,
    };

    let accounts = client.get_program_accounts(telemetry_program_id, options)?;

    let mut result = HashMap::new();

    for (pubkey, account) in accounts {
        match DeviceLatencySamples::try_from(&account.data[..]) {
            Ok(latency_data) => {
                result.insert(pubkey, latency_data);
            }
            Err(_) => {
                // Skip accounts that fail to deserialize
                continue;
            }
        }
    }

    Ok(result)
}
