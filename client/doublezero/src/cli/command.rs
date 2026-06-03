use clap::{Args, Subcommand};
use clap_complete::Shell;
use doublezero_geolocation_cli::GeolocationArgs;
use doublezero_serviceability_cli::cli::ServiceabilityCommand;

use crate::{
    cli::multicast::MulticastCliCommand,
    command::{
        connect::ProvisioningCliCommand, disable::DisableCliCommand,
        disconnect::DecommissioningCliCommand, enable::EnableCliCommand,
        latency::LatencyCliCommand, routes::RoutesCliCommand, status::StatusCliCommand,
    },
};

/// Top-level command tree for the unified `doublezero` binary.
///
/// Per RFC-20 §Module contract item 2, the serviceability verbs live in
/// `doublezero_serviceability_cli::cli::ServiceabilityCommand` and are hoisted
/// to the top level here via `#[command(flatten)]`. The binary retains the
/// daemon-control verbs, the `doublezero-geolocation-cli` module crate's
/// geolocation subtree (via `GeolocationArgs`), the binary-only `Completion`
/// generator, and `Multicast` (whose `Subscribe`/`Unsubscribe`/`Publish`/
/// `Unpublish` arms depend on binary-local daemon-control infrastructure).
#[derive(Subcommand, Debug)]
pub enum Command {
    /// Connect your server to a doublezero device
    Connect(ProvisioningCliCommand),
    /// Enable the reconciler (start managing tunnels)
    Enable(EnableCliCommand),
    /// Disable the reconciler (tear down tunnels and stop managing them)
    Disable(DisableCliCommand),
    /// Get the status of your service
    Status(StatusCliCommand),
    /// Disconnect your server from the doublezero network
    Disconnect(DecommissioningCliCommand),
    /// Get device latencies
    Latency(LatencyCliCommand),
    /// View your installed routes
    Routes(RoutesCliCommand),

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
