//! Resolved, read-only configuration shared between the binary and every
//! module crate.
//!
//! Per RFC-20 (§CliContext): the binary populates `CliContext` once at
//! startup from `--env` plus any explicit flag or environment-variable
//! overrides. Modules treat the value as read-only. The context carries
//! resolved values only — URLs, paths, identifiers, the signer path, and
//! the output-format hint — never live clients.

use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;
use std::path::PathBuf;

use doublezero_config::Environment;

/// The output-format hint carried by `CliContext`.
///
/// Verbs continue to own their own `--json` / `--json-compact` flags per RFC
/// §Output ("per-command `--json` keeps coupling to the binary low"). This
/// hint exists so the binary can communicate a default when a verb chooses to
/// honor it.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
pub enum OutputFormat {
    #[default]
    Table,
    Json,
    JsonCompact,
}

/// Resolved configuration carried from the binary into every module verb.
#[derive(Debug, Clone)]
pub struct CliContext {
    /// Selected environment (e.g., `Devnet`).
    pub env: Environment,

    /// DZ ledger RPC URL (HTTPS).
    pub ledger_rpc_url: String,
    /// DZ ledger WebSocket URL.
    pub ledger_ws_rpc_url: String,
    /// Solana L1 RPC URL.
    pub solana_l1_rpc_url: String,

    /// Serviceability program ID.
    pub serviceability_program_id: Pubkey,
    /// Geolocation program ID.
    pub geolocation_program_id: Pubkey,
    /// Telemetry program ID.
    pub telemetry_program_id: Pubkey,

    /// Path to the signer keypair file, if provided.
    ///
    /// Modules construct their own `Keypair` from this path lazily; the
    /// context never holds keypair material directly (RFC-20 §Security).
    pub keypair_path: Option<PathBuf>,

    /// Daemon Unix socket path, if provided.
    pub daemon_socket_path: Option<PathBuf>,

    /// Default output-format hint.
    pub output_format: OutputFormat,
}

/// Builder for `CliContext`. The binary populates a builder from parsed
/// global flags and per-field overrides; the builder applies `--env`-derived
/// defaults from `doublezero-config` and yields a fully resolved context.
#[derive(Debug, Default)]
pub struct CliContextBuilder {
    env: Option<Environment>,
    ledger_rpc_url: Option<String>,
    ledger_ws_rpc_url: Option<String>,
    solana_l1_rpc_url: Option<String>,
    serviceability_program_id: Option<Pubkey>,
    geolocation_program_id: Option<Pubkey>,
    telemetry_program_id: Option<Pubkey>,
    keypair_path: Option<PathBuf>,
    daemon_socket_path: Option<PathBuf>,
    output_format: OutputFormat,
}

impl CliContextBuilder {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn with_env(mut self, env: Environment) -> Self {
        self.env = Some(env);
        self
    }

    pub fn with_ledger_rpc_url(mut self, url: impl Into<String>) -> Self {
        self.ledger_rpc_url = Some(url.into());
        self
    }

    pub fn with_ledger_ws_rpc_url(mut self, url: impl Into<String>) -> Self {
        self.ledger_ws_rpc_url = Some(url.into());
        self
    }

    pub fn with_solana_l1_rpc_url(mut self, url: impl Into<String>) -> Self {
        self.solana_l1_rpc_url = Some(url.into());
        self
    }

    pub fn with_serviceability_program_id(mut self, id: Pubkey) -> Self {
        self.serviceability_program_id = Some(id);
        self
    }

    pub fn with_geolocation_program_id(mut self, id: Pubkey) -> Self {
        self.geolocation_program_id = Some(id);
        self
    }

    pub fn with_telemetry_program_id(mut self, id: Pubkey) -> Self {
        self.telemetry_program_id = Some(id);
        self
    }

    pub fn with_keypair_path(mut self, path: PathBuf) -> Self {
        self.keypair_path = Some(path);
        self
    }

    pub fn with_daemon_socket_path(mut self, path: PathBuf) -> Self {
        self.daemon_socket_path = Some(path);
        self
    }

    pub fn with_output_format(mut self, format: OutputFormat) -> Self {
        self.output_format = format;
        self
    }

    /// Resolve all fields and produce a `CliContext`.
    ///
    /// If `env` is set and a given override is `None`, the corresponding
    /// value is sourced from the `doublezero-config` `NetworkConfig` for that
    /// environment. If `env` is unset, the caller must supply every URL and
    /// program-ID field explicitly.
    pub fn build(self) -> eyre::Result<CliContext> {
        let env = self.env.unwrap_or_default();
        let config = env.config()?;

        Ok(CliContext {
            env,
            ledger_rpc_url: self.ledger_rpc_url.unwrap_or(config.ledger_public_rpc_url),
            ledger_ws_rpc_url: self
                .ledger_ws_rpc_url
                .unwrap_or(config.ledger_public_ws_rpc_url),
            solana_l1_rpc_url: self.solana_l1_rpc_url.unwrap_or(config.solana_l1_rpc_url),
            serviceability_program_id: self
                .serviceability_program_id
                .unwrap_or(config.serviceability_program_id),
            geolocation_program_id: self
                .geolocation_program_id
                .unwrap_or(config.geolocation_program_id),
            telemetry_program_id: self
                .telemetry_program_id
                .unwrap_or(config.telemetry_program_id),
            keypair_path: self.keypair_path,
            daemon_socket_path: self.daemon_socket_path,
            output_format: self.output_format,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serial_test::serial;

    #[test]
    #[serial]
    fn builder_resolves_from_env_defaults() {
        let ctx = CliContextBuilder::new()
            .with_env(Environment::MainnetBeta)
            .build()
            .unwrap();
        assert_eq!(ctx.env, Environment::MainnetBeta);
        assert!(ctx.ledger_rpc_url.starts_with("https://"));
        assert!(ctx.ledger_ws_rpc_url.starts_with("wss://"));
        assert!(!ctx.solana_l1_rpc_url.is_empty());
        assert_eq!(ctx.output_format, OutputFormat::Table);
        assert!(ctx.keypair_path.is_none());
    }

    #[test]
    #[serial]
    fn builder_per_field_overrides_win_over_env() {
        let ctx = CliContextBuilder::new()
            .with_env(Environment::Devnet)
            .with_ledger_rpc_url("https://custom-rpc.example/")
            .build()
            .unwrap();
        assert_eq!(ctx.ledger_rpc_url, "https://custom-rpc.example/");
    }
}
