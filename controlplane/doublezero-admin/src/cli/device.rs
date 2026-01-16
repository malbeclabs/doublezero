use clap::{Args, Subcommand};
use doublezero_cli::device::{
    create::CreateDeviceCliCommand,
    delete::DeleteDeviceCliCommand,
    get::GetDeviceCliCommand,
    interface::{
        create::CreateDeviceInterfaceCliCommand, delete::DeleteDeviceInterfaceCliCommand,
        get::GetDeviceInterfaceCliCommand, list::ListDeviceInterfaceCliCommand,
        update::UpdateDeviceInterfaceCliCommand,
    },
    list::ListDeviceCliCommand,
    update::UpdateDeviceCliCommand,
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
    /// Delete a device
    #[clap()]
    Delete(DeleteDeviceCliCommand),
    /// Interface commands
    #[clap()]
    Interface(InterfaceCliCommand),
}
