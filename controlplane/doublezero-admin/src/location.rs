use clap::Args;
use clap::Subcommand;

use doublezero_cli::location::create::*;
use doublezero_cli::location::update::*;
use doublezero_cli::location::list::*;
use doublezero_cli::location::get::*;
use doublezero_cli::location::delete::*;

#[derive(Args, Debug)]
pub struct LocationArgs {
    #[command(subcommand)]
    pub command: LocationCommands,
}

#[derive(Debug, Subcommand)]
pub enum LocationCommands {
    Create(CreateLocationArgs),
    Update(UpdateLocationArgs),
    List(ListLocationArgs),
    Get(GetLocationArgs),
    Delete(DeleteLocationArgs)
}

