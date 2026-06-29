use doublezero_program_common::types::parse_utils::bandwidth_to_string;
use solana_sdk::pubkey::Pubkey;

const NANOS_TO_MS: f32 = 1_000_000.0;

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

/// Number of leading characters kept when abbreviating a pubkey or key for
/// narrow table output.
const SHORT_PREFIX_LEN: usize = 10;

/// Canonical narrow-output abbreviation: keep the leading [`SHORT_PREFIX_LEN`]
/// characters + `..` (or the string unchanged when it is no longer than that).
/// A contiguous prefix stays copyable for prefix searches in explorers, and the
/// char-based slice never splits a multibyte boundary. New `--narrow` columns
/// should abbreviate pubkeys and embedded keys through here (or
/// [`display_pubkey_short`]); multicast group codes use `abbreviate_name`
/// (a first/last split that preserves both ends of the code) instead.
pub fn abbreviate_prefix(s: &str) -> String {
    if s.chars().count() > SHORT_PREFIX_LEN + 2 {
        let prefix: String = s.chars().take(SHORT_PREFIX_LEN).collect();
        format!("{prefix}..")
    } else {
        s.to_string()
    }
}

/// Abbreviate a pubkey for narrow table output via [`abbreviate_prefix`]
/// (e.g. `7Np3kR9xQ2..`).
pub fn display_pubkey_short(pk: &Pubkey) -> String {
    abbreviate_prefix(&pk.to_string())
}

pub fn display_string_vec(v: &[String]) -> String {
    v.join(", ")
}
