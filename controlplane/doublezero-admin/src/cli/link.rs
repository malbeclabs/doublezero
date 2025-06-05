use clap::{Args, Subcommand};

use doublezero_cli::link::{create::*, delete::*, get::*, list::*, update::*};

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
