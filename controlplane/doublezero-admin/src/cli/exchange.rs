use clap::{Args, Subcommand};

use doublezero_cli::exchange::{create::*, delete::*, get::*, list::*, update::*};

#[derive(Args, Debug)]
pub struct ExchangeCliCommand {
    #[command(subcommand)]
    pub command: ExchangeCommands,
}

#[derive(Debug, Subcommand)]
pub enum ExchangeCommands {
    /// Create a new exchange
    #[clap()]
    Create(CreateExchangeCliCommand),
    /// Update an existing exchange
    #[clap()]
    Update(UpdateExchangeCliCommand),
    /// List all exchanges
    #[clap()]
    List(ListExchangeCliCommand),
    /// Get details for a specific exchange
    #[clap()]
    Get(GetExchangeCliCommand),
    /// Delete an exchange
    #[clap()]
    Delete(DeleteExchangeCliCommand),
}
