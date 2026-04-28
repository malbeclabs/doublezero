use clap::{Args, Subcommand};

use super::multicastgroup::MulticastGroupCliCommand;

#[derive(Args, Debug)]
pub struct MulticastCliCommand {
    #[command(subcommand)]
    pub command: MulticastCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastCommands {
    /// Manage multicast groups
    #[clap()]
    Group(MulticastGroupCliCommand),
    /// Subscribe to one or more multicast groups (user must already be connected)
    #[clap()]
    Subscribe(MulticastSubscribeCliCommand),
    /// Unsubscribe from one or more multicast groups
    #[clap()]
    Unsubscribe(MulticastUnsubscribeCliCommand),
    /// Publish to one or more multicast groups (user must already be connected)
    #[clap()]
    Publish(MulticastPublishCliCommand),
    /// Stop publishing to one or more multicast groups
    #[clap()]
    Unpublish(MulticastUnpublishCliCommand),
}

#[derive(Args, Debug)]
pub struct MulticastSubscribeCliCommand {
    /// Multicast group code(s) to subscribe to
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}

#[derive(Args, Debug)]
pub struct MulticastUnsubscribeCliCommand {
    /// Multicast group code(s) to unsubscribe from
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}

#[derive(Args, Debug)]
pub struct MulticastPublishCliCommand {
    /// Multicast group code(s) to publish to
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}

#[derive(Args, Debug)]
pub struct MulticastUnpublishCliCommand {
    /// Multicast group code(s) to stop publishing to
    #[arg(num_args = 1..)]
    pub groups: Vec<String>,
}
