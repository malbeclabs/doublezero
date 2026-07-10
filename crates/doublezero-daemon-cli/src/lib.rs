//! RFC-20 module crate for daemon-control verbs (`connect`, `disconnect`,
//! `status`, `enable`, `disable`, `latency`, `routes`, and the multicast
//! transport verbs `subscribe`/`unsubscribe`/`publish`/`unpublish`).
//!
//! See `rfcs/rfc20-cli-standardization.md` and `docs/cli-standard.md`.

pub mod cli;
pub mod client;
pub mod connect;
pub mod disable;
pub mod disconnect;
pub mod enable;
pub mod helpers;
pub mod latency;
pub mod ledger;
pub mod multicast;
mod requirements;
pub mod routes;
pub mod status;

pub use cli::DaemonCommand;
pub use client::{DaemonClient, DaemonClientImpl};
pub use ledger::LedgerClient;
// The multicast verbs are NOT `DaemonCommand` variants: the binary nests them
// under its `multicast` subtree to preserve the `doublezero multicast <verb>`
// invocation (hoisting would be a breaking CLI change).
pub use multicast::{Publish, Subscribe, Unpublish, Unsubscribe};
