use clap::{Args, Subcommand};
use doublezero_cli::{
    allowlist::device::{
        add::AddDeviceAllowlistCliCommand, list::ListDeviceAllowlistCliCommand,
        remove::RemoveDeviceAllowlistCliCommand,
    },
    device::{
        create::CreateDeviceCliCommand, delete::DeleteDeviceCliCommand, get::GetDeviceCliCommand,
        list::ListDeviceCliCommand, resume::ResumeDeviceCliCommand,
        suspend::SuspendDeviceCliCommand, update::UpdateDeviceCliCommand,
    },
};

#[derive(Args, Debug)]
pub struct DeviceCliCommand {
    #[command(subcommand)]
    pub command: DeviceCommands,
}

#[derive(Debug, Subcommand)]
pub enum DeviceCommands {
    /// Create a new device
    #[clap()]
    Create(CreateDeviceCliCommand),
    /// Update an existing device
    #[clap()]
    Update(UpdateDeviceCliCommand),
    /// List all devices
    #[clap()]
    List(ListDeviceCliCommand),
    /// Get details for a specific device
    #[clap()]
    Get(GetDeviceCliCommand),
    /// Suspend a device
    #[clap()]
    Suspend(SuspendDeviceCliCommand),
    /// Resume a suspended device
    #[clap()]
    Resume(ResumeDeviceCliCommand),
    /// Delete a device
    #[clap()]
    Delete(DeleteDeviceCliCommand),
    /// Manage device allowlist
    #[clap()]
    Allowlist(DeviceAllowlistCliCommand),
}

#[derive(Args, Debug)]
pub struct DeviceAllowlistCliCommand {
    #[command(subcommand)]
    pub command: DeviceAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum DeviceAllowlistCommands {
    /// List device allowlist
    #[clap()]
    List(ListDeviceAllowlistCliCommand),
    /// Add a device to the allowlist
    #[clap()]
    Add(AddDeviceAllowlistCliCommand),
    /// Remove a device from the allowlist
    #[clap()]
    Remove(RemoveDeviceAllowlistCliCommand),
}
