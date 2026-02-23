use clap::{Args, Subcommand};
use doublezero_cli::geolocation::config::{
    get::GetGeoConfigCliCommand, set::SetGeoConfigCliCommand,
};

#[derive(Args, Debug)]
pub struct ConfigCliCommand {
    #[command(subcommand)]
    pub command: ConfigCommands,
}

#[derive(Subcommand, Debug)]
pub enum ConfigCommands {
    /// Display current configuration
    Get(GetGeoConfigCliCommand),
    /// Update configuration values
    Set(SetGeoConfigCliCommand),
}
