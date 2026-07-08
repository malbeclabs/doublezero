//! RFC-20 module crate for daemon-control verbs (`connect`, `disconnect`,
//! `status`, `enable`, `disable`, `latency`, `routes`).
//!
//! See `rfcs/rfc20-cli-standardization.md` and `docs/cli-standard.md`.

pub mod cli;
pub mod client;
pub mod disable;
pub mod disconnect;
pub mod enable;
pub mod helpers;
pub mod ledger;
mod requirements;
pub mod status;

pub use cli::DaemonCommand;
pub use client::{DaemonClient, DaemonClientImpl};
pub use ledger::LedgerClient;
