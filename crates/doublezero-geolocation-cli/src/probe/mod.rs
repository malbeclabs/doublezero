pub mod add_parent;
pub mod create;
pub mod delete;
pub mod get;
pub mod list;
pub mod remove_parent;
pub mod update;

use clap::{Args, Subcommand};

use add_parent::AddParentGeoProbeCliCommand;
use create::CreateGeoProbeCliCommand;
use delete::DeleteGeoProbeCliCommand;
use get::GetGeoProbeCliCommand;
use list::ListGeoProbeCliCommand;
use remove_parent::RemoveParentGeoProbeCliCommand;
use update::UpdateGeoProbeCliCommand;

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
