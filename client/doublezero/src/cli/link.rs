use clap::{Args, Subcommand};

use doublezero_cli::link::{
    accept::AcceptLinkCliCommand, delete::*, dzx_create::CreateDZXLinkCliCommand, get::*,
    latency::LatencyCliCommand, list::*, update::*, wan_create::*,
};

#[derive(Args, Debug)]
pub struct LinkCliCommand {
    #[command(subcommand)]
    pub command: LinkCommands,
}

#[derive(Debug, Subcommand)]
pub enum CreateLinkCommands {
    /// Create a new WAN link
    #[clap()]
    Wan(CreateWANLinkCliCommand),
    /// Create a new DZX link
    #[clap()]
    Dzx(CreateDZXLinkCliCommand),
}

#[derive(Args, Debug)]
pub struct CreateLinkCommand {
    #[command(subcommand)]
    pub command: CreateLinkCommands,
}

#[derive(Debug, Subcommand)]
pub enum LinkCommands {
    /// Create a new link
    #[clap()]
    Create(CreateLinkCommand),
    /// Accept a link
    #[clap()]
    Accept(AcceptLinkCliCommand),
    /// Update an existing link
    #[clap()]
    Update(UpdateLinkCliCommand),
    /// List all links
    #[clap()]
    List(ListLinkCliCommand),
    /// Measure latency for links
    #[clap()]
    Latency(LatencyCliCommand),
    /// Get details for a specific link
    #[clap()]
    Get(GetLinkCliCommand),
    /// Delete a link
    #[clap()]
    Delete(DeleteLinkCliCommand),
}
