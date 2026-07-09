//! Top-level daemon-control subcommand tree per RFC-20.
//!
//! Mounted flat (`#[command(flatten)]`) — the binary's `Command` enum hoists
//! all variants so verbs surface as `doublezero <verb>` (e.g. `doublezero
//! connect`, `doublezero status`).

use clap::Subcommand;
use doublezero_cli_core::CliContext;
use std::io::Write;

use crate::{
    client::DaemonClient, connect::Connect, disable::Disable, disconnect::Disconnect,
    enable::Enable, latency::Latency, ledger::LedgerClient, routes::Routes, status::Status,
};

/// Daemon-control verbs hoisted to the binary's top level.
///
/// Populated incrementally as verbs migrate from the binary into this crate.
#[derive(Subcommand, Debug)]
pub enum DaemonCommand {
    /// Connect your server to a doublezero device
    Connect(Connect),
    /// Enable the reconciler (start managing tunnels)
    Enable(Enable),
    /// Disable the reconciler (tear down tunnels and stop managing them)
    Disable(Disable),
    /// Get the status of your service
    Status(Status),
    /// Disconnect your server from the doublezero network
    Disconnect(Disconnect),
    /// Get device latencies
    Latency(Latency),
    /// View your installed routes
    Routes(Routes),
}

impl DaemonCommand {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        ctx: &CliContext,
        daemon: &D,
        ledger: &L,
        out: &mut W,
    ) -> eyre::Result<()> {
        match self {
            Self::Connect(cmd) => cmd.execute(ctx, daemon, ledger, out).await,
            Self::Enable(cmd) => cmd.execute(ctx, daemon, ledger, out).await,
            Self::Disable(cmd) => cmd.execute(ctx, daemon, ledger, out).await,
            Self::Status(cmd) => cmd.execute(ctx, daemon, ledger, out).await,
            Self::Disconnect(cmd) => cmd.execute(ctx, daemon, ledger, out).await,
            Self::Latency(cmd) => cmd.execute(ctx, daemon, ledger, out).await,
            Self::Routes(cmd) => cmd.execute(ctx, daemon, ledger, out).await,
        }
    }
}
