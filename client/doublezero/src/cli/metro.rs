use clap::{Args, Subcommand};

use doublezero_cli::metro::{
    create::*, delete::*, get::*, list::*, setdevice::SetDeviceMetroCliCommand, update::*,
};

#[derive(Args, Debug)]
pub struct MetroCliCommand {
    #[command(subcommand)]
    pub command: MetroCommands,
}

#[derive(Debug, Subcommand)]
pub enum MetroCommands {
    /// Create a new metro
    #[clap()]
    Create(CreateMetroCliCommand),
    /// Update an existing metro
    #[clap()]
    Update(UpdateMetroCliCommand),
    /// Set devices for a metro
    #[clap()]
    SetDevice(SetDeviceMetroCliCommand),
    /// List all metros
    #[clap()]
    List(ListMetroCliCommand),
    /// Get details for a specific metro
    #[clap()]
    Get(GetMetroCliCommand),
    /// Delete a metro
    #[clap()]
    Delete(DeleteMetroCliCommand),
}
