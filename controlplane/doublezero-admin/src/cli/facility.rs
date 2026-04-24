use clap::{Args, Subcommand};

use doublezero_cli::facility::{create::*, delete::*, get::*, list::*, update::*};

#[derive(Args, Debug)]
pub struct FacilityCliCommand {
    #[command(subcommand)]
    pub command: FacilityCommands,
}

#[derive(Debug, Subcommand)]
pub enum FacilityCommands {
    /// Create a new facility
    #[clap()]
    Create(CreateFacilityCliCommand),
    /// Update an existing facility
    #[clap()]
    Update(UpdateFacilityCliCommand),
    /// List all facilities
    #[clap()]
    List(ListFacilityCliCommand),
    /// Get details for a specific facility
    #[clap()]
    Get(GetFacilityCliCommand),
    /// Delete a facility
    #[clap()]
    Delete(DeleteFacilityCliCommand),
}
