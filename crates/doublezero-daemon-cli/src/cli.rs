//! Top-level daemon-control subcommand tree per RFC-20.
//!
//! Mounted flat (`#[command(flatten)]`) — the binary's `Command` enum hoists
//! all variants so verbs surface as `doublezero <verb>` (e.g. `doublezero
//! connect`, `doublezero status`).

use clap::Subcommand;
use doublezero_cli_core::CliContext;
use std::io::Write;

use crate::{client::DaemonClient, ledger::LedgerClient};

/// Daemon-control verbs hoisted to the binary's top level.
///
/// Populated incrementally as verbs migrate from the binary into this crate.
#[derive(Subcommand, Debug)]
pub enum DaemonCommand {}

impl DaemonCommand {
    pub async fn execute<D: DaemonClient, L: LedgerClient, W: Write>(
        self,
        _ctx: &CliContext,
        _daemon: &D,
        _ledger: &L,
        _out: &mut W,
    ) -> eyre::Result<()> {
        match self {}
    }
}
