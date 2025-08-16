use clap::{Args, Subcommand};

use crate::command::config::SetConfigCliCommand;
use doublezero_cli::config::get::GetConfigCliCommand;

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
