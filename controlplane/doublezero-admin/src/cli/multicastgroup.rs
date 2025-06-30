use clap::{Args, Subcommand};

use doublezero_cli::multicastgroup::{
    allowlist::{
        publisher::{
            add::AddMulticastGroupPubAllowlistCliCommand,
            list::ListMulticastGroupPubAllowlistCliCommand,
            remove::RemoveMulticastGroupPubAllowlistCliCommand,
        },
        subscriber::{
            add::AddMulticastGroupSubAllowlistCliCommand,
            list::ListMulticastGroupSubAllowlistCliCommand,
            remove::RemoveMulticastGroupSubAllowlistCliCommand,
        },
    },
    create::CreateMulticastGroupCliCommand,
    delete::DeleteMulticastGroupCliCommand,
    get::GetMulticastGroupCliCommand,
    list::ListMulticastGroupCliCommand,
    update::UpdateMulticastGroupCliCommand,
};

#[derive(Args, Debug)]
pub struct MulticastGroupCliCommand {
    #[command(subcommand)]
    pub command: MulticastGroupCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastGroupCommands {
    /// Manage multicast group allowlists
    #[clap()]
    Allowlist(MulticastGroupAllowlistCliCommand),
    /// Create a new multicast group
    #[clap()]
    Create(CreateMulticastGroupCliCommand),
    /// Update an existing multicast group
    #[clap()]
    Update(UpdateMulticastGroupCliCommand),
    /// List all multicast groups
    #[clap()]
    List(ListMulticastGroupCliCommand),
    /// Get details for a specific multicast group
    #[clap()]
    Get(GetMulticastGroupCliCommand),
    /// Delete a multicast group
    #[clap()]
    Delete(DeleteMulticastGroupCliCommand),
}

#[derive(Args, Debug)]
pub struct MulticastGroupAllowlistCliCommand {
    #[command(subcommand)]
    pub command: MulticastGroupAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastGroupAllowlistCommands {
    /// Manage publisher allowlist for multicast group
    #[clap()]
    Publisher(MulticastGroupPubAllowlistCliCommand),
    /// Manage subscriber allowlist for multicast group
    #[clap()]
    Subscriber(MulticastGroupSubAllowlistCliCommand),
}

#[derive(Args, Debug)]
pub struct MulticastGroupPubAllowlistCliCommand {
    #[command(subcommand)]
    pub command: MulticastGroupPubAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastGroupPubAllowlistCommands {
    /// List publisher allowlist
    #[clap()]
    List(ListMulticastGroupPubAllowlistCliCommand),
    /// Add a publisher to the allowlist
    #[clap()]
    Add(AddMulticastGroupPubAllowlistCliCommand),
    /// Remove a publisher from the allowlist
    #[clap()]
    Remove(RemoveMulticastGroupPubAllowlistCliCommand),
}

#[derive(Args, Debug)]
pub struct MulticastGroupSubAllowlistCliCommand {
    #[command(subcommand)]
    pub command: MulticastGroupSubAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastGroupSubAllowlistCommands {
    /// List subscriber allowlist
    #[clap()]
    List(ListMulticastGroupSubAllowlistCliCommand),
    /// Add a subscriber to the allowlist
    #[clap()]
    Add(AddMulticastGroupSubAllowlistCliCommand),
    /// Remove a subscriber from the allowlist
    #[clap()]
    Remove(RemoveMulticastGroupSubAllowlistCliCommand),
}
