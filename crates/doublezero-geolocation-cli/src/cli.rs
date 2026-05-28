//! Top-level `doublezero geolocation` subcommand tree per RFC-20.
//!
//! Mounted nested (not flattened) — the binary's `Command` enum carries a
//! single `Geolocation(GeolocationCommand)` variant so verbs surface as
//! `doublezero geolocation <subcommand>`.

use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;
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

/// Entry-point `Args` struct for the `doublezero geolocation` subtree.
///
/// Clap requires an `Args`-implementing struct to carry a nested `Subcommand`
/// enum; the binary mounts this as `Command::Geolocation(GeolocationArgs)`.
#[derive(Args, Debug)]
pub struct GeolocationArgs {
    #[command(subcommand)]
    pub command: GeolocationCommand,
}

impl GeolocationCommand {
    pub async fn execute<C: GeoCliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        match self {
            Self::Init(args) => args.execute(client, out),
            Self::Probe(cmd) => match cmd.command {
                ProbeCommands::Create(args) => args.execute(ctx, client, out).await,
                ProbeCommands::Update(args) => args.execute(ctx, client, out).await,
                ProbeCommands::Delete(args) => args.execute(ctx, client, out).await,
                ProbeCommands::Get(args) => args.execute(ctx, client, out).await,
                ProbeCommands::List(args) => args.execute(ctx, client, out).await,
                ProbeCommands::AddParent(args) => args.execute(ctx, client, out).await,
                ProbeCommands::RemoveParent(args) => args.execute(ctx, client, out).await,
            },
            Self::User(cmd) => match cmd.command {
                UserCommands::Create(args) => args.execute(ctx, client, out).await,
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
