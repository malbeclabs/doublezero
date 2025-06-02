use clap::Args;
use clap::Subcommand;

use doublezero_cli::link::create::*;
use doublezero_cli::link::delete::*;
use doublezero_cli::link::get::*;
use doublezero_cli::link::list::*;
use doublezero_cli::link::update::*;

#[derive(Args, Debug)]
pub struct LinkCliCommand {
    #[command(subcommand)]
    pub command: LinkCommands,
}

#[derive(Debug, Subcommand)]
pub enum LinkCommands {
    Create(CreateLinkCliCommand),
    Update(UpdateLinkCliCommand),
    List(ListLinkCliCommand),
    Get(GetLinkCliCommand),
    Delete(DeleteLinkCliCommand),
}
