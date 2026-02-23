use clap::{Args, Subcommand};
use doublezero_cli::geolocation::probe::{
    add_parent::AddParentGeoProbeCliCommand, create::CreateGeoProbeCliCommand,
    delete::DeleteGeoProbeCliCommand, get::GetGeoProbeCliCommand, list::ListGeoProbeCliCommand,
    remove_parent::RemoveParentGeoProbeCliCommand, update::UpdateGeoProbeCliCommand,
};

#[derive(Args, Debug)]
pub struct ProbeCliCommand {
    #[command(subcommand)]
    pub command: ProbeCommands,
}

#[derive(Subcommand, Debug)]
pub enum ProbeCommands {
    /// Create a new geolocation probe
    Create(CreateGeoProbeCliCommand),
    /// Update an existing probe
    Update(UpdateGeoProbeCliCommand),
    /// Delete a probe
    Delete(DeleteGeoProbeCliCommand),
    /// Get details of a specific probe
    Get(GetGeoProbeCliCommand),
    /// List all probes
    List(ListGeoProbeCliCommand),
    /// Add a parent device to a probe
    AddParent(AddParentGeoProbeCliCommand),
    /// Remove a parent device from a probe
    RemoveParent(RemoveParentGeoProbeCliCommand),
}
