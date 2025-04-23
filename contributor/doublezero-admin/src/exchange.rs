use clap::Args;
use clap::Subcommand;

use double_zero_cli::exchange::create::*;
use double_zero_cli::exchange::update::*;
use double_zero_cli::exchange::list::*;
use double_zero_cli::exchange::get::*;
use double_zero_cli::exchange::delete::*;



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
    Delete(DeleteExchangeArgs)
}

