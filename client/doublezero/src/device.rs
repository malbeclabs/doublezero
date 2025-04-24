use clap::Args;
use clap::Subcommand;

use doublezero_cli::device::create::CreateDeviceArgs;
use doublezero_cli::device::update::UpdateDeviceArgs;
use doublezero_cli::device::list::ListDeviceArgs;
use doublezero_cli::device::get::GetDeviceArgs;
use doublezero_cli::device::delete::DeleteDeviceArgs;
use doublezero_cli::allowlist::device::list::ListDeviceAllowlistArgs;
use doublezero_cli::allowlist::device::add::AddDeviceAllowlistArgs;
use doublezero_cli::allowlist::device::remove::RemoveDeviceAllowlistArgs;

#[derive(Args, Debug)]
pub struct DeviceArgs {
    #[command(subcommand)]
    pub command: DeviceCommands,
}

#[derive(Debug, Subcommand)]
pub enum DeviceCommands {
    Create(CreateDeviceArgs),
    Update(UpdateDeviceArgs),
    List(ListDeviceArgs),
    Get(GetDeviceArgs),
    Delete(DeleteDeviceArgs),
    Allowlist(DeviceAllowlistArgs),
}


#[derive(Args, Debug)]
pub struct DeviceAllowlistArgs {
    #[command(subcommand)]
    pub command: DeviceAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum DeviceAllowlistCommands {
    List(ListDeviceAllowlistArgs),
    Add(AddDeviceAllowlistArgs),
    Remove(RemoveDeviceAllowlistArgs),
}
