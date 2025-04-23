use clap::Args;
use clap::Subcommand;

use double_zero_cli::location::create::*;
use double_zero_cli::location::update::*;
use double_zero_cli::location::list::*;
use double_zero_cli::location::get::*;
use double_zero_cli::location::delete::*;

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

