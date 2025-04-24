use clap::Args;
use clap::Subcommand;

use doublezero_cli::globalconfig::set::*;
use doublezero_cli::globalconfig::get::*;

use doublezero_cli::allowlist::foundation::get::*;
use doublezero_cli::allowlist::foundation::add::*;
use doublezero_cli::allowlist::foundation::remove::*;



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
