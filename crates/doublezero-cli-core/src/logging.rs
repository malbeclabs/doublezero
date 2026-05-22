//! Diagnostic-logging facade.
//!
//! Per RFC-20 (§Diagnostic logging): diagnostic output goes to standard
//! error through `tracing`. The binary configures the global log level from
//! `--verbose`; modules use the standard log macros (`debug!`, `info!`,
//! `warn!`, `error!`, `trace!`) for anything that explains what a verb is
//! doing internally. JSON output on stdout stays parseable because logs go
//! to stderr.

use tracing_subscriber::EnvFilter;

/// Configure the global `tracing` subscriber from a verbosity count.
///
/// - `0` → `warn` (default for non-verbose runs)
/// - `1` → `debug` (`-v`)
/// - `2` or more → `trace` (`-vv`)
///
/// If the `RUST_LOG` environment variable is set, it overrides the verbosity
/// argument; this matches what operators expect from standard Rust logging
/// stacks and makes it possible to tune per-module log levels from the
/// environment without changing CLI flags.
///
/// Logs are written to standard error so command output on standard output
/// remains parseable when combined with `--json`.
///
/// Safe to call multiple times: subsequent calls are no-ops because the
/// subscriber registration uses `try_init`.
pub fn init_logging(verbosity: u8) {
    let filter = if let Ok(env_filter) = EnvFilter::try_from_default_env() {
        env_filter
    } else {
        let level = match verbosity {
            0 => "warn",
            1 => "debug",
            _ => "trace",
        };
        EnvFilter::new(level)
    };

    let _ = tracing_subscriber::fmt()
        .with_env_filter(filter)
        .with_writer(std::io::stderr)
        .with_target(false)
        .try_init();
}
