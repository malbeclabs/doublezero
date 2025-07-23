use clap::{Args, Subcommand};

use doublezero_cli::globalstate::set_internet_latency_collector::SetInternetLatencyCollectorCliCommand;

#[derive(Args, Debug)]
pub struct GlobalStateCliCommand {
    #[command(subcommand)]
    pub command: GlobalStateCommands,
}

#[derive(Debug, Subcommand)]
pub enum GlobalStateCommands {
    /// Set the internet latency collector pubkey in global state
    #[clap()]
    SetInternetLatencyCollector(SetInternetLatencyCollectorCliCommand),
}
