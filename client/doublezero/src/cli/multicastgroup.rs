use clap::Args;
use clap::Subcommand;

use doublezero_cli::multicastgroup::create::*;
use doublezero_cli::multicastgroup::delete::*;
use doublezero_cli::multicastgroup::get::*;
use doublezero_cli::multicastgroup::list::*;
use doublezero_cli::multicastgroup::update::*;

#[derive(Args, Debug)]
pub struct MulticastGroupCliCommand {
    #[command(subcommand)]
    pub command: MulticastGroupCommands,
}

#[derive(Debug, Subcommand)]
pub enum MulticastGroupCommands {
    Create(CreateMulticastGroupCliCommand),
    Update(UpdateMulticastGroupCliCommand),
    List(ListMulticastGroupCliCommand),
    Get(GetMulticastGroupCliCommand),
    Delete(DeleteMulticastGroupCliCommand),
}
