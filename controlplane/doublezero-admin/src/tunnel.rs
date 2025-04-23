use clap::Args;
use clap::Subcommand;

use double_zero_cli::tunnel::create::*;
use double_zero_cli::tunnel::update::*;
use double_zero_cli::tunnel::list::*;
use double_zero_cli::tunnel::get::*;
use double_zero_cli::tunnel::delete::*;




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
