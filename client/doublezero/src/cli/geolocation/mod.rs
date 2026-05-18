use clap::{Args, Subcommand};

pub mod probe;

use probe::ProbeCliCommand;

#[derive(Args, Debug)]
pub struct GeolocationCliCommand {
    #[command(subcommand)]
    pub command: GeolocationCommands,
}

#[derive(Subcommand, Debug)]
pub enum GeolocationCommands {
    /// Manage geolocation probes
    Probe(ProbeCliCommand),
}
