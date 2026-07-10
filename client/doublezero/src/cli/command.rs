use clap::{Args, Subcommand};
use clap_complete::Shell;
use doublezero_daemon_cli::DaemonCommand;
use doublezero_geolocation_cli::GeolocationArgs;
use doublezero_serviceability_cli::cli::ServiceabilityCommand;

use crate::cli::{multicast::MulticastCliCommand, sentinel::SentinelCliCommand};

/// Top-level command tree for the unified `doublezero` binary.
///
/// Per RFC-20 §Module contract item 2, module-crate verbs are hoisted to the
/// top level via `#[command(flatten)]`: serviceability verbs from
/// `doublezero_serviceability_cli`, daemon-control verbs (`connect`, `enable`,
/// `disable`, `status`, `disconnect`, `latency`, `routes`) from
/// `doublezero_daemon_cli`. The binary retains the
/// `doublezero-geolocation-cli` module crate's geolocation subtree (via
/// `GeolocationArgs`), the binary-only `Completion` generator, and `Multicast`
/// (whose `Subscribe`/`Unsubscribe`/`Publish`/`Unpublish` arms route to
/// `doublezero-daemon-cli` but stay nested to preserve the
/// `doublezero multicast <verb>` invocation).
#[derive(Subcommand, Debug)]
pub enum Command {
    /// Daemon-control verbs migrated to `doublezero-daemon-cli` (RFC-20).
    /// Hoisted to top-level via `#[command(flatten)]`.
    #[command(flatten)]
    Daemon(DaemonCommand),

    /// Sentinel admin commands
    #[command(hide = true)]
    Sentinel(SentinelCliCommand),

    /// Manage geolocation probes and users
    Geolocation(GeolocationArgs),

    /// Manage multicast
    Multicast(MulticastCliCommand),

    /// Generate shell completions
    Completion(CompletionCliCommand),

    /// Flattened serviceability variants (Device, Link, Location, User, ...).
    /// Hoisted to top-level via `#[command(flatten)]`.
    #[command(flatten)]
    Serviceability(ServiceabilityCommand),
}

#[derive(Args, Debug, Clone)]
pub struct CompletionCliCommand {
    #[arg(value_enum)]
    pub shell: Shell,
}
