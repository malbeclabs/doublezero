use clap::Args;
use clap::Subcommand;

use doublezero_cli::location::create::*;
use doublezero_cli::location::delete::*;
use doublezero_cli::location::get::*;
use doublezero_cli::location::list::*;
use doublezero_cli::location::update::*;

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
