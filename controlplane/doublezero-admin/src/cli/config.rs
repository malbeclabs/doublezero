use clap::{Args, Subcommand};

use doublezero_cli::config::{get::GetConfigCliCommand, set::SetConfigCliCommand};

#[derive(Args, Debug)]
pub struct ConfigCliCommand {
    #[command(subcommand)]
    pub command: ConfigCommands,
}

#[derive(Debug, Subcommand)]
pub enum ConfigCommands {
    #[command(about = "Get current config settings", hide = false)]
    Get(GetConfigCliCommand),
    #[command(about = "Set a config setting", hide = false)]
    Set(SetConfigCliCommand),
}
