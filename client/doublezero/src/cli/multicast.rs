use clap::{Args, Subcommand};

use doublezero_daemon_cli::multicast::{Publish, Subscribe, Unpublish, Unsubscribe};
use doublezero_serviceability_cli::cli::multicastgroup::MulticastGroupCliCommand;

#[derive(Args, Debug)]
pub struct MulticastCliCommand {
    #[command(subcommand)]
    pub command: MulticastCommands,
}

/// Multicast subtree: `Group` CRUD dispatches to the serviceability module
/// crate; the transport verbs (`Subscribe`/`Unsubscribe`/`Publish`/`Unpublish`)
/// dispatch to `doublezero-daemon-cli`. They are nested here — not hoisted as
/// `DaemonCommand` variants — to preserve the `doublezero multicast <verb>`
/// invocation.
#[derive(Debug, Subcommand)]
pub enum MulticastCommands {
    /// Manage multicast groups
    #[clap()]
    Group(MulticastGroupCliCommand),
    /// Subscribe to one or more multicast groups (user must already be connected)
    #[clap()]
    Subscribe(Subscribe),
    /// Unsubscribe from one or more multicast groups
    #[clap()]
    Unsubscribe(Unsubscribe),
    /// Publish to one or more multicast groups (user must already be connected)
    #[clap()]
    Publish(Publish),
    /// Stop publishing to one or more multicast groups
    #[clap()]
    Unpublish(Unpublish),
}
