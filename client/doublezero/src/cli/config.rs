use clap::{Args, Subcommand};

use doublezero_cli::config::{get::GetConfigCliCommand, set::SetConfigCliCommand};

#[derive(Args, Debug)]
pub struct ConfigCliCommand {
    #[command(subcommand)]
    pub command: ConfigCommands,
}

#[derive(Debug, Subcommand)]
pub enum ConfigCommands {
    /// Get current config settings
    #[command()]
    Get(GetConfigCliCommand),
    /// Set a config setting
    #[command()]
    Set(SetConfigCliCommand),
}
