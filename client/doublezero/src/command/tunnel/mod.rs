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
pub struct TunnelArgs {
    #[command(subcommand)]
    pub command: TunnelCommands,
}

#[derive(Debug, Subcommand)]
pub enum TunnelCommands {
    Create(CreateTunnelArgs),
    Update(UpdateTunnelArgs),
    List(ListTunnelArgs),
    Get(GetTunnelArgs),
    Delete(DeleteTunnelArgs)
}
