use super::probe::ProbeCliCommand;
use clap::Subcommand;
use doublezero_cli::geolocation::programconfig::init::InitProgramConfigCliCommand;

#[derive(Subcommand, Debug)]
pub enum Command {
    /// Manage geolocation probes
    Probe(ProbeCliCommand),
    /// Initialize the geolocation program config (one-time setup)
    InitConfig(InitProgramConfigCliCommand),
}
