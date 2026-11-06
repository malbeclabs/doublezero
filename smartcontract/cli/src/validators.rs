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

use doublezero_serviceability::helper::is_global;
use std::net::Ipv4Addr;

/// Parses a public IPv4 address: one the serviceability program accepts as a
/// device's public or agent IP (`is_global`). Private, loopback, link-local,
/// CGNAT, documentation, multicast, reserved and unspecified addresses fail.
pub fn validate_public_ipv4(val: &str) -> Result<Ipv4Addr, String> {
    let ip: Ipv4Addr = val
        .parse()
        .map_err(|_| format!("invalid IPv4 address: {val}"))?;
    if !is_global(ip) {
        return Err(format!("not a public IPv4 address: {ip}"));
    }
    Ok(ip)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_validate_public_ipv4_accepts_global() {
        for val in ["1.2.3.4", "5.6.7.8", "2.3.4.5"] {
            assert_eq!(validate_public_ipv4(val), Ok(val.parse().unwrap()), "{val}");
        }
    }

    #[test]
    fn test_validate_public_ipv4_rejects_non_global() {
        for val in [
            "0.0.0.0",
            "0.1.2.3",
            "10.0.0.1",
            "100.64.0.1",
            "127.0.0.1",
            "169.254.0.1",
            "172.16.0.1",
            "192.0.0.1",
            "192.0.2.1",
            "192.168.1.1",
            "198.51.100.1",
            "203.0.113.1",
            "224.0.0.1",
            "240.0.0.1",
            "255.255.255.255",
        ] {
            assert_eq!(
                validate_public_ipv4(val),
                Err(format!("not a public IPv4 address: {val}"))
            );
        }
    }

    #[test]
    fn test_validate_public_ipv4_rejects_malformed() {
        for val in [
            "",
            "192.0.2",
            "192.0.2.256",
            "2001:db8::1",
            "192.0.2.1/32",
            "host",
        ] {
            assert_eq!(
                validate_public_ipv4(val),
                Err(format!("invalid IPv4 address: {val}"))
            );
        }
    }
}
