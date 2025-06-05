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
    Create(CreateDeviceCliCommand),
    Update(UpdateDeviceCliCommand),
    List(ListDeviceCliCommand),
    Get(GetDeviceCliCommand),
    Suspend(SuspendDeviceCliCommand),
    Resume(ResumeDeviceCliCommand),
    Delete(DeleteDeviceCliCommand),
    Allowlist(DeviceAllowlistCliCommand),
}

#[derive(Args, Debug)]
pub struct DeviceAllowlistCliCommand {
    #[command(subcommand)]
    pub command: DeviceAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum DeviceAllowlistCommands {
    List(ListDeviceAllowlistCliCommand),
    Add(AddDeviceAllowlistCliCommand),
    Remove(RemoveDeviceAllowlistCliCommand),
}
