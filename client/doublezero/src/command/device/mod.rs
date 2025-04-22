use clap::Args;
use clap::Subcommand;

use self::create::*;
use self::update::*;
use self::list::*;
use self::get::*;
use self::delete::*;
use self::allowlist::*;

pub mod create;
pub mod update;
pub mod list;
pub mod get;
pub mod delete;
pub mod allowlist;

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
