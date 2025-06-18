use clap::{Args, Subcommand};

use doublezero_cli::link::{create::*, delete::*, get::*, list::*, update::*};

#[derive(Args, Debug)]
pub struct LinkCliCommand {
    #[command(subcommand)]
    pub command: LinkCommands,
}

#[derive(Debug, Subcommand)]
pub enum LinkCommands {
    /// Create a new link
    #[clap()]
    Create(CreateLinkCliCommand),
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
