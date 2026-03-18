use super::{config::ConfigCliCommand, probe::ProbeCliCommand, user::UserCliCommand};
use clap::Subcommand;
use doublezero_cli::geolocation::programconfig::init::InitProgramConfigCliCommand;

#[derive(Subcommand, Debug)]
pub enum Command {
    /// Display or update CLI configuration
    Config(ConfigCliCommand),
    /// Manage geolocation probes
    Probe(ProbeCliCommand),
    /// Manage geolocation users and targets
    User(UserCliCommand),
    /// Initialize the geolocation program config (one-time setup)
    InitConfig(InitProgramConfigCliCommand),
}
