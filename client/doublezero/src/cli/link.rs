use clap::{Args, Subcommand};
use doublezero_cli::{
    link::{
        accept::AcceptLinkCliCommand, delete::*, dzx_create::CreateDZXLinkCliCommand, get::*,
        latency::LinkLatencyCliCommand, list::*, sethealth::SetLinkHealthCliCommand, update::*,
        wan_create::*,
    },
    topology::{
        backfill::BackfillTopologyCliCommand, clear::ClearTopologyCliCommand,
        create::CreateTopologyCliCommand, delete::DeleteTopologyCliCommand,
        list::ListTopologyCliCommand,
    },
};

#[derive(Args, Debug)]
pub struct LinkCliCommand {
    #[command(subcommand)]
    pub command: LinkCommands,
}

#[derive(Debug, Subcommand)]
pub enum CreateLinkCommands {
    /// Create a new WAN link
    #[clap()]
    Wan(CreateWANLinkCliCommand),
    /// Create a new DZX link
    #[clap()]
    Dzx(CreateDZXLinkCliCommand),
}

#[derive(Args, Debug)]
pub struct CreateLinkCommand {
    #[command(subcommand)]
    pub command: CreateLinkCommands,
}

#[derive(Debug, Subcommand)]
pub enum LinkCommands {
    /// Create a new link
    #[clap()]
    Create(CreateLinkCommand),
    /// Accept a link
    #[clap()]
    Accept(AcceptLinkCliCommand),
    /// Update an existing link
    #[clap()]
    Update(UpdateLinkCliCommand),
    /// List all links
    #[clap()]
    List(ListLinkCliCommand),
    /// Get details for a specific link
    #[clap()]
    Get(GetLinkCliCommand),
    /// Display latency statistics for a link
    #[clap()]
    Latency(LinkLatencyCliCommand),
    /// Delete a link
    Delete(DeleteLinkCliCommand),
    /// Set the health status of a link interface
    // Hidden because this is an internal/operational command not intended for general CLI users.
    #[clap(hide = true)]
    SetHealth(SetLinkHealthCliCommand),
    /// Manage link topologies
    #[clap()]
    Topology(TopologyLinkCommand),
}

#[derive(Args, Debug)]
pub struct TopologyLinkCommand {
    #[command(subcommand)]
    pub command: TopologyCommands,
}

#[derive(Debug, Subcommand)]
pub enum TopologyCommands {
    /// Create a new topology
    Create(CreateTopologyCliCommand),
    /// Delete a topology
    Delete(DeleteTopologyCliCommand),
    /// Clear a topology from links
    Clear(ClearTopologyCliCommand),
    /// Backfill FlexAlgoNodeSegment entries on existing Vpnv4 loopbacks
    Backfill(BackfillTopologyCliCommand),
    /// List all topologies
    List(ListTopologyCliCommand),
}
