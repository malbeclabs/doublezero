use clap::{Args, Subcommand};

use doublezero_cli::location::{create::*, delete::*, get::*, list::*, update::*};

#[derive(Args, Debug)]
pub struct LocationCliCommand {
    #[command(subcommand)]
    pub command: LocationCommands,
}

#[derive(Debug, Subcommand)]
pub enum LocationCommands {
    /// Create a new location
    #[clap()]
    Create(CreateLocationCliCommand),
    /// Update an existing location
    #[clap()]
    Update(UpdateLocationCliCommand),
    /// List all locations
    #[clap()]
    List(ListLocationCliCommand),
    /// Get details for a specific location
    #[clap()]
    Get(GetLocationCliCommand),
    /// Delete a location
    #[clap()]
    Delete(DeleteLocationCliCommand),
}
