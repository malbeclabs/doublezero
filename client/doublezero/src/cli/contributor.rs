use clap::{Args, Subcommand};

use doublezero_cli::contributor::{create::*, delete::*, get::*, list::*, update::*};

#[derive(Args, Debug)]
pub struct ContributorCliCommand {
    #[command(subcommand)]
    pub command: ContributorCommands,
}

#[derive(Debug, Subcommand)]
pub enum ContributorCommands {
    /// Create a new contributor
    #[clap()]
    Create(CreateContributorCliCommand),
    /// Update an existing contributor
    #[clap()]
    Update(UpdateContributorCliCommand),
    /// List all contributors
    #[clap()]
    List(ListContributorCliCommand),
    /// Get details for a specific contributor
    #[clap()]
    Get(GetContributorCliCommand),
    /// Delete a contributor
    #[clap()]
    Delete(DeleteContributorCliCommand),
}
