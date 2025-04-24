use clap::Args;
use clap::Subcommand;

use doublezero_cli::globalconfig::set::*;
use doublezero_cli::globalconfig::get::*;

use doublezero_cli::allowlist::foundation::list::ListFoundationAllowlistArgs;
use doublezero_cli::allowlist::foundation::add::AddFoundationAllowlistArgs;
use doublezero_cli::allowlist::foundation::remove::RemoveFoundationAllowlistArgs;



#[derive(Args, Debug)]
pub struct GlobalConfigArgs {
    #[command(subcommand)]
    pub command: GlobalConfigCommands,
}

#[derive(Debug, Subcommand)]
pub enum GlobalConfigCommands {
    Get(GetGlobalConfigArgs),
    Set(SetGlobalConfigArgs),
    Allowlist(FoundationAllowlistArgs),
}


#[derive(Args, Debug)]
pub struct FoundationAllowlistArgs {
    #[command(subcommand)]
    pub command: FoundationAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum FoundationAllowlistCommands {
    List(ListFoundationAllowlistArgs),
    Add(AddFoundationAllowlistArgs),
    Remove(RemoveFoundationAllowlistArgs),
}
