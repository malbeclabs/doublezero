use clap::{Args, Subcommand};

use doublezero_cli::link::{
    accept::AcceptLinkCliCommand, delete::*, external_create::CreateExternalLinkCliCommand, get::*,
    internal_create::*, list::*, update::*,
};

#[derive(Args, Debug)]
pub struct LinkCliCommand {
    #[command(subcommand)]
    pub command: LinkCommands,
}

#[derive(Debug, Subcommand)]
pub enum CreateLinkCommands {
    /// Create an internal new link
    #[clap()]
    Internal(CreateInternalLinkCliCommand),
    /// Create an internal new link
    #[clap()]
    External(CreateExternalLinkCliCommand),
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
    /// Get details for a specific link
    #[clap()]
    Get(GetLinkCliCommand),
    /// Delete a link
    #[clap()]
    Delete(DeleteLinkCliCommand),
}
