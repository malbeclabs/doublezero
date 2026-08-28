//! Typed errors for the passport CLI verbs.
//!
//! This crate is a reusable library, so it owns neither `anyhow` nor `eyre`.
//! Every verb returns [`PassportCliError`] and each consumer lifts it into its
//! own application error with a single `?` (the `solana-cli` adapter into
//! `anyhow`, the unified `doublezero` binary into `eyre`). Because every
//! conversion is a real `From` rather than a `"{e:#}"` string flatten, the cause
//! chain survives all the way to the binary instead of collapsing into one line.

use solana_sdk::pubkey::Pubkey;

/// Errors surfaced by the passport verbs.
#[derive(Debug, thiserror::Error)]
pub enum PassportCliError {
    // Boxed to keep the enum small (`ClientError` is ~260 bytes); see
    // `clippy::result_large_err`. A manual `From<ClientError>` does the boxing
    // so `?` still works at call sites.
    #[error(transparent)]
    Rpc(Box<solana_client::client_error::ClientError>),

    #[error(transparent)]
    ParsePubkey(#[from] solana_sdk::pubkey::ParsePubkeyError),

    #[error(transparent)]
    ParseSignature(#[from] solana_sdk::signature::ParseSignatureError),

    #[error(transparent)]
    ParseUrl(#[from] url::ParseError),

    #[error("failed to parse IP address: {0}")]
    ParseIp(#[from] std::net::AddrParseError),

    #[error(transparent)]
    Utf8(#[from] std::str::Utf8Error),

    #[error(transparent)]
    Json(#[from] serde_json::Error),

    #[error(transparent)]
    Io(#[from] std::io::Error),

    #[error(transparent)]
    Sentinel(#[from] doublezero_ledger_sentinel::Error),

    #[error("Unable to fetch cluster nodes. Is your RPC endpoint correct?")]
    ClusterNodesUnavailable,

    #[error("Failed to resolve an IPv4 address")]
    Ipv4ResolutionFailed,

    #[error("Failed to extract the IP from the response")]
    IpExtractionFailed,

    #[error("Access request not found for service key {service_key}")]
    AccessRequestNotFound {
        service_key: Pubkey,
        #[source]
        source: Box<dyn std::error::Error + Send + Sync>,
    },

    #[error("Access request already exists: {0}")]
    AccessRequestExists(Pubkey),

    #[error("Signature verification failed")]
    SignatureVerificationFailed,

    /// Catch-all for foreign errors surfaced by dependencies that expose
    /// `anyhow::Error` (the `Wallet` transaction helpers, zero-copy account
    /// fetches, instruction building) or other boxed sources. The boxed value
    /// keeps the underlying cause chain intact, so `?`-lifting into `anyhow`
    /// or `eyre` at the boundary still prints the full chain.
    #[error(transparent)]
    Other(Box<dyn std::error::Error + Send + Sync>),
}

impl From<solana_client::client_error::ClientError> for PassportCliError {
    fn from(err: solana_client::client_error::ClientError) -> Self {
        PassportCliError::Rpc(Box::new(err))
    }
}

/// Convenience alias used throughout the crate.
pub type Result<T> = std::result::Result<T, PassportCliError>;

impl PassportCliError {
    /// Wrap a foreign error (typically `anyhow::Error` from a dependency) into
    /// [`PassportCliError::Other`], preserving its cause chain.
    pub fn other<E>(err: E) -> Self
    where
        E: Into<Box<dyn std::error::Error + Send + Sync>>,
    {
        PassportCliError::Other(err.into())
    }
}
