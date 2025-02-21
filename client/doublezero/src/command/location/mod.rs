use clap::Args;
use clap::Subcommand;

use self::create::*;
use self::update::*;
use self::list::*;
use self::get::*;
use self::delete::*;

pub mod create;
pub mod update;
pub mod list;
pub mod get;
pub mod delete;


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
