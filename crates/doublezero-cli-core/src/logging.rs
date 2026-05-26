//! Diagnostic-logging facade.
//!
//! Per RFC-20 (§Diagnostic logging): diagnostic output goes to standard
//! error through `tracing`. The binary configures the global log level from
//! `--log-level`; modules use the standard log macros (`debug!`, `info!`,
//! `warn!`, `error!`, `trace!`) for anything that explains what a verb is
//! doing internally. JSON output on stdout stays parseable because logs go
//! to stderr.

use clap::ValueEnum;
use tracing_subscriber::EnvFilter;

/// Diagnostic log level selectable from `--log-level`.
///
/// Mirrors the `tracing` level hierarchy plus an explicit `Off` that silences
/// every level. `Warn` is the default for non-verbose runs; `Debug` and
/// `Trace` are the levels operators reach for when chasing a bug.
#[derive(Copy, Clone, Debug, Default, Eq, PartialEq, ValueEnum)]
#[value(rename_all = "lower")]
pub enum LogLevel {
    Off,
    Error,
    #[default]
    Warn,
    Info,
    Debug,
    Trace,
}

impl LogLevel {
    fn as_filter_str(self) -> &'static str {
        match self {
            LogLevel::Off => "off",
            LogLevel::Error => "error",
            LogLevel::Warn => "warn",
            LogLevel::Info => "info",
            LogLevel::Debug => "debug",
            LogLevel::Trace => "trace",
        }
    }
}

/// Configure the global `tracing` subscriber from a `LogLevel`.
///
/// If the `RUST_LOG` environment variable is set, it overrides the level
/// argument; this matches what operators expect from standard Rust logging
/// stacks and makes it possible to tune per-module log levels from the
/// environment without changing CLI flags.
///
/// Logs are written to standard error so command output on standard output
/// remains parseable when combined with `--json`.
///
/// Safe to call multiple times: subsequent calls are no-ops because the
/// subscriber registration uses `try_init`.
pub fn init_logging(level: LogLevel) {
    let filter = if let Ok(env_filter) = EnvFilter::try_from_default_env() {
        env_filter
    } else {
        EnvFilter::new(level.as_filter_str())
    };

    let _ = tracing_subscriber::fmt()
        .with_env_filter(filter)
        .with_writer(std::io::stderr)
        .with_target(false)
        .try_init();
}
