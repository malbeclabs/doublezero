//! Error and result types for CLI modules.
//!
//! Per RFC-20 (§Error handling and requirements): "All `execute` functions
//! return a fallible result. The binary catches the top-level error and
//! renders a single-line message followed by a chain of causes."
//!
//! `Result` aliases `eyre::Result` so existing modules that already use eyre
//! can adopt this crate without churn. `CliError` is a `thiserror`-based
//! enum for the small set of structured errors the core helpers produce
//! themselves (missing keypair, malformed env-var override, etc.); module
//! errors continue to flow as `eyre::Report`.

use thiserror::Error;

/// Result alias used across `doublezero-cli-core` and module verbs.
pub type Result<T> = eyre::Result<T>;

/// Structured errors produced by `doublezero-cli-core` helpers.
#[derive(Debug, Error)]
pub enum CliError {
    #[error("no keypair available: {0}")]
    MissingKeypair(String),

    #[error("invalid environment variable {name}: {reason}")]
    InvalidEnvVar { name: String, reason: String },
}

/// Render a top-level error to stderr as a single-line message followed by
/// the chain of causes, matching RFC-20 §Error handling.
///
/// Intended to be called once at the binary's top-level error handler.
pub fn render_error<E>(err: &E)
where
    E: AsRef<dyn std::error::Error>,
{
    let err: &dyn std::error::Error = err.as_ref();
    eprintln!("Error: {err}");
    let mut source = err.source();
    while let Some(cause) = source {
        eprintln!("  caused by: {cause}");
        source = cause.source();
    }
}

/// Render an `eyre::Report` using its native chain iteration (preferred when
/// the binary uses eyre, since it keeps wrapped contexts).
pub fn render_eyre(err: &eyre::Report) {
    let mut chain = err.chain();
    if let Some(first) = chain.next() {
        eprintln!("Error: {first}");
    }
    for cause in chain {
        eprintln!("  caused by: {cause}");
    }
}
