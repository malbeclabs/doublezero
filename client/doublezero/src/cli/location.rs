use clap::{Args, Subcommand};

use doublezero_cli::location::{create::*, delete::*, get::*, list::*, update::*};

#[derive(Args, Debug)]
pub struct LocationCliCommand {
    #[command(subcommand)]
    pub command: LocationCommands,
}

#[derive(Debug, Subcommand)]
pub enum LocationCommands {
    Create(CreateLocationCliCommand),
    Update(UpdateLocationCliCommand),
    List(ListLocationCliCommand),
    Get(GetLocationCliCommand),
    Delete(DeleteLocationCliCommand),
}
