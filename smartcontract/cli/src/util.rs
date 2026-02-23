use doublezero_program_common::types::parse_utils::bandwidth_to_string;
use solana_sdk::pubkey::Pubkey;

const NANOS_TO_MS: f32 = 1000000.0;

pub fn display_as_ms(latency: &u64) -> String {
    format!("{:.2}ms", (*latency as f32 / NANOS_TO_MS))
}

pub fn display_pks(pks: &[Pubkey]) -> String {
    pks.iter()
        .map(|pk| pk.to_string())
        .collect::<Vec<_>>()
        .join(",")
}

pub fn display_count(pks: &[Pubkey]) -> String {
    pks.len().to_string()
}

pub fn display_as_bandwidth(bandwidth: &u64) -> String {
    bandwidth_to_string(bandwidth)
}
