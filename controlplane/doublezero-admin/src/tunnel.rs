use clap::Args;
use clap::Subcommand;

use doublezero_cli::tunnel::create::*;
use doublezero_cli::tunnel::update::*;
use doublezero_cli::tunnel::list::*;
use doublezero_cli::tunnel::get::*;
use doublezero_cli::tunnel::delete::*;




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
