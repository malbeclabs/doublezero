//! Test helpers for module-crate unit tests.
//!
//! Per RFC-20 (§Testing conventions): "The CLI core crate SHOULD ship the
//! test helpers needed to make verb unit tests and per-module integration
//! tests low-cost." This module is the seed of that helper set; it grows as
//! the conforming-verb pattern is rolled out across modules.

use solana_sdk::pubkey::Pubkey;

use doublezero_config::Environment;

use crate::context::{CliContext, CliContextBuilder, OutputFormat};

/// Build a `CliContext` suitable for unit tests against a mocked client.
///
/// The returned context uses the `Local` environment by default. Override
/// any field by chaining additional `with_*` builder methods before calling
/// `.build()` — for example:
///
/// ```ignore
/// use doublezero_cli_core::testing::cli_context_for_tests;
/// let ctx = cli_context_for_tests().with_env(Environment::Devnet).build()?;
/// ```
pub fn cli_context_for_tests() -> CliContextBuilder {
    CliContextBuilder::new()
        .with_env(Environment::Local)
        .with_ledger_rpc_url("http://localhost:8899")
        .with_ledger_ws_rpc_url("ws://localhost:8900")
        .with_solana_l1_rpc_url("http://localhost:8899")
        .with_serviceability_program_id(Pubkey::new_unique())
        .with_geolocation_program_id(Pubkey::new_unique())
        .with_telemetry_program_id(Pubkey::new_unique())
        .with_output_format(OutputFormat::Table)
}

/// Convenience: build a fully resolved `CliContext` with sensible defaults
/// for unit tests. Tests that need to override specific fields should use
/// `cli_context_for_tests()` and chain builder methods.
pub fn cli_context_default_for_tests() -> CliContext {
    cli_context_for_tests()
        .build()
        .expect("default test context must resolve")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_test_context_is_local() {
        let ctx = cli_context_default_for_tests();
        assert_eq!(ctx.env, Environment::Local);
        assert_eq!(ctx.ledger_rpc_url, "http://localhost:8899");
    }

    #[test]
    fn builder_overrides_apply() {
        let ctx = cli_context_for_tests()
            .with_ledger_rpc_url("http://override.test")
            .build()
            .unwrap();
        assert_eq!(ctx.ledger_rpc_url, "http://override.test");
    }
}
