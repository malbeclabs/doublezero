use crate::command::edge::{EdgeDisableCliCommand, EdgeEnableCliCommand, EdgeStatusCliCommand};
use clap::{Args, Subcommand};

#[derive(Args, Debug)]
pub struct EdgeCliCommand {
    #[command(subcommand)]
    pub command: EdgeCommands,
}

#[derive(Debug, Subcommand)]
pub enum EdgeCommands {
    /// Enable edge feed parsing for a multicast group
    #[command()]
    Enable(EdgeEnableCliCommand),
    /// Disable edge feed parsing for a multicast group
    #[command()]
    Disable(EdgeDisableCliCommand),
    /// Show status of active edge feed parsers
    #[command()]
    Status(EdgeStatusCliCommand),
}
