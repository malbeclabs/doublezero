//! Re-export of the shared `clap` value-parser validators.
//!
//! Per RFC-20 (§Module contract item 6): "Modules MAY define module-specific
//! validators, but the shared validators for pubkey, code, bandwidth,
//! latency, and IPv4 MUST be used wherever those types appear." The
//! implementations now live in `doublezero-cli-core`; this module preserves
//! the existing import path (`use doublezero_serviceability_cli::validators::*`) so the
//! serviceability crate's call sites and any external consumer continue to
//! compile unchanged.

pub use doublezero_cli_core::validators::{
    validate_code, validate_parse_bandwidth, validate_parse_delay_ms,
    validate_parse_delay_override_ms, validate_parse_jitter_ms, validate_pubkey,
    validate_pubkey_or_code,
};
