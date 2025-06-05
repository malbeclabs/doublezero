use clap::{Args, Subcommand};

use doublezero_cli::exchange::{create::*, delete::*, get::*, list::*, update::*};

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
