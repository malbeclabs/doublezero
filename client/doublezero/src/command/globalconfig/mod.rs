use allowlist::AllowlistArgs;
use clap::Args;
use clap::Subcommand;

use self::set::*;
use self::get::*;

pub mod set;
pub mod get;
pub mod allowlist;


#[derive(Args, Debug)]
pub struct GlobalConfigArgs {
    #[command(subcommand)]
    pub command: GlobalConfigCommands,
}

#[derive(Debug, Subcommand)]
pub enum GlobalConfigCommands {
    Get(GetGlobalConfigArgs),
    Set(SetGlobalConfigArgs),
    Allowlist(AllowlistArgs),
}