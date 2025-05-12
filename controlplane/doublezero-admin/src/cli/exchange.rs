use clap::Args;
use clap::Subcommand;

use doublezero_cli::exchange::create::*;
use doublezero_cli::exchange::delete::*;
use doublezero_cli::exchange::get::*;
use doublezero_cli::exchange::list::*;
use doublezero_cli::exchange::update::*;

#[derive(Args, Debug)]
pub struct ExchangeCliCommand {
    #[command(subcommand)]
    pub command: ExchangeCommands,
}

#[derive(Debug, Subcommand)]
pub enum ExchangeCommands {
    Create(CreateExchangeCliCommand),
    Update(UpdateExchangeCliCommand),
    List(ListExchangeCliCommand),
    Get(GetExchangeCliCommand),
    Delete(DeleteExchangeCliCommand),
}
