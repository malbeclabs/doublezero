use clap::{Args, Subcommand};
use doublezero_cli::{
    allowlist::device::{
        add::AddDeviceAllowlistCliCommand, list::ListDeviceAllowlistCliCommand,
        remove::RemoveDeviceAllowlistCliCommand,
    },
    device::{
        create::CreateDeviceCliCommand,
        delete::DeleteDeviceCliCommand,
        get::GetDeviceCliCommand,
        interface::{
            create::CreateDeviceInterfaceCliCommand, delete::DeleteDeviceInterfaceCliCommand,
            get::GetDeviceInterfaceCliCommand, list::ListDeviceInterfaceCliCommand,
            update::UpdateDeviceInterfaceCliCommand,
        },
        list::ListDeviceCliCommand,
        resume::ResumeDeviceCliCommand,
        suspend::SuspendDeviceCliCommand,
        update::UpdateDeviceCliCommand,
    },
};

#[derive(Debug, Subcommand)]
pub enum InterfaceCommands {
    /// Create a new device interface
    #[clap()]
    Create(CreateDeviceInterfaceCliCommand),
    /// Update an existing device interface
    #[clap()]
    Update(UpdateDeviceInterfaceCliCommand),
    /// List all device interfaces for a given device
    #[clap()]
    List(ListDeviceInterfaceCliCommand),
    /// Get details for a specific device interface
    #[clap()]
    Get(GetDeviceInterfaceCliCommand),
    /// Delete a device interface
    #[clap()]
    Delete(DeleteDeviceInterfaceCliCommand),
}

#[derive(Args, Debug)]
pub struct InterfaceCliCommand {
    #[command(subcommand)]
    pub command: InterfaceCommands,
}

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
    /// Interface commands
    #[clap()]
    Interface(InterfaceCliCommand),
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
