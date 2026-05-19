use clap::{Args, Subcommand};

pub mod probe;
pub mod user;

use probe::ProbeCliCommand;
use user::UserCliCommand;

#[derive(Args, Debug)]
pub struct GeolocationCliCommand {
    #[command(subcommand)]
    pub command: GeolocationCommands,
}

#[derive(Subcommand, Debug)]
pub enum GeolocationCommands {
    /// Manage geolocation probes
    Probe(ProbeCliCommand),
    /// Manage geolocation users and targets
    User(UserCliCommand),
}
