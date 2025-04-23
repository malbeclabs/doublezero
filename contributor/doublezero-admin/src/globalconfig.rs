use clap::Args;
use clap::Subcommand;

use double_zero_cli::globalconfig::set::*;
use double_zero_cli::globalconfig::get::*;

use double_zero_cli::globalconfig::allowlist::get::*;
use double_zero_cli::globalconfig::allowlist::add::*;
use double_zero_cli::globalconfig::allowlist::remove::*;



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
