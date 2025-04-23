use clap::Args;
use clap::Subcommand;

use doublezero_cli::device::create::CreateDeviceArgs;
use doublezero_cli::device::update::UpdateDeviceArgs;
use doublezero_cli::device::list::ListDeviceArgs;
use doublezero_cli::device::get::GetDeviceArgs;
use doublezero_cli::device::delete::DeleteDeviceArgs;
use doublezero_cli::device::allowlist::get::GetAllowlistArgs;
use doublezero_cli::device::allowlist::add::AddAllowlistArgs;
use doublezero_cli::device::allowlist::remove::RemoveAllowlistArgs;

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
    Allowlist(AllowlistArgs),
}


#[derive(Args, Debug)]
pub struct AllowlistArgs {
    #[command(subcommand)]
    pub command: AllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum AllowlistCommands {
    Get(GetAllowlistArgs),
    Add(AddAllowlistArgs),
    Remove(RemoveAllowlistArgs),
}
