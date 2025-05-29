use clap::Args;
use clap::Subcommand;

use doublezero_cli::allowlist::device::add::AddDeviceAllowlistCliCommand;
use doublezero_cli::allowlist::device::list::ListDeviceAllowlistCliCommand;
use doublezero_cli::allowlist::device::remove::RemoveDeviceAllowlistCliCommand;
use doublezero_cli::device::create::CreateDeviceCliCommand;
use doublezero_cli::device::delete::DeleteDeviceCliCommand;
use doublezero_cli::device::get::GetDeviceCliCommand;
use doublezero_cli::device::list::ListDeviceCliCommand;
use doublezero_cli::device::update::UpdateDeviceCliCommand;

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
    Reactivate(ReactivateDeviceCliCommand),
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
