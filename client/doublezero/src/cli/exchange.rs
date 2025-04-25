use clap::Args;
use clap::Subcommand;

use doublezero_cli::exchange::create::*;
use doublezero_cli::exchange::delete::*;
use doublezero_cli::exchange::get::*;
use doublezero_cli::exchange::list::*;
use doublezero_cli::exchange::update::*;

#[derive(Args, Debug)]
pub struct ExchangeArgs {
    #[command(subcommand)]
    pub command: ExchangeCommands,
}

#[derive(Debug, Subcommand)]
pub enum ExchangeCommands {
    Create(CreateExchangeArgs),
    Update(UpdateExchangeArgs),
    List(ListExchangeArgs),
    Get(GetExchangeArgs),
    Delete(DeleteExchangeArgs),
}
