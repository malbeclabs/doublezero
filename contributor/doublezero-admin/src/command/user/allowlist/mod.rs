use clap::Args;
use clap::Subcommand;

use self::get::*;
use self::add::*;
use self::remove::*;

pub mod add;
pub mod remove;
pub mod get;


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