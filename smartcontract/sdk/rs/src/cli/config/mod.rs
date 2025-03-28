use clap::Args;
use clap::Subcommand;

use self::get::*;
use self::set::*;

pub mod get;
pub mod set;

#[derive(Args, Debug)]
pub struct ConfigArgs {
    #[command(subcommand)]
    pub command: ConfigCommands,
}

#[derive(Debug, Subcommand)]
pub enum ConfigCommands {
    #[command(about = "Get current config settings", hide = false)]
    Get(GetConfigArgs),
    #[command(about = "Set a config setting", hide = false)]
    Set(SetConfigArgs),
}
