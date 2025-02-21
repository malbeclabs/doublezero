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
