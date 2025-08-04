use clap::{Args, Subcommand};

use doublezero_cli::exchange::{
    create::*, delete::*, get::*, list::*, setdevice::SetDeviceExchangeCliCommand, update::*,
};

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
    /// Set devices for an exchange
    #[clap()]
    SetDevice(SetDeviceExchangeCliCommand),
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
