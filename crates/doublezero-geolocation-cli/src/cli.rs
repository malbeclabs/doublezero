//! Top-level `doublezero geolocation` subcommand tree per RFC-20.
//!
//! Mounted nested (not flattened) — the binary's `Command` enum carries a
//! single `Geolocation(GeolocationCommand)` variant so verbs surface as
//! `doublezero geolocation <subcommand>`.

use clap::{Args, Subcommand};
use std::io::Write;

use crate::{
    client::GeoCliCommand,
    init::InitProgramConfigCliCommand,
    probe::{ProbeCliCommand, ProbeCommands},
    user::{UserCliCommand, UserCommands},
};

/// Top-level enum mounted by the binary.
#[derive(Subcommand, Debug)]
pub enum GeolocationCommand {
    /// Initialize the geolocation program config (one-time)
    #[command(hide = true)]
    Init(InitProgramConfigCliCommand),
    /// Manage geolocation probes
    Probe(ProbeCliCommand),
    /// Manage geolocation users and targets
    User(UserCliCommand),
}

/// Wrapper retained for the migration period so the binary can mount
/// `Command::Geolocation(GeolocationCliCommand)` while Task 3 lands.
/// Remove once the binary mounts `GeolocationCommand` directly.
#[derive(Args, Debug)]
pub struct GeolocationCliCommand {
    #[command(subcommand)]
    pub command: GeolocationCommand,
}

impl GeolocationCommand {
    pub fn execute<C: GeoCliCommand, W: Write>(
        self,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        match self {
            Self::Init(args) => args.execute(client, out),
            Self::Probe(cmd) => match cmd.command {
                ProbeCommands::Create(args) => args.execute(client, out),
                ProbeCommands::Update(args) => args.execute(client, out),
                ProbeCommands::Delete(args) => args.execute(client, out),
                ProbeCommands::Get(args) => args.execute(client, out),
                ProbeCommands::List(args) => args.execute(client, out),
                ProbeCommands::AddParent(args) => args.execute(client, out),
                ProbeCommands::RemoveParent(args) => args.execute(client, out),
            },
            Self::User(cmd) => match cmd.command {
                UserCommands::Create(args) => args.execute(client, out),
                UserCommands::Update(args) => args.execute(client, out),
                UserCommands::Delete(args) => args.execute(client, out),
                UserCommands::Get(args) => args.execute(client, out),
                UserCommands::List(args) => args.execute(client, out),
                UserCommands::AddTarget(args) => args.execute(client, out),
                UserCommands::RemoveTarget(args) => args.execute(client, out),
                UserCommands::SetResultDestination(args) => args.execute(client, out),
                UserCommands::UpdatePayment(args) => args.execute(client, out),
            },
        }
    }
}
