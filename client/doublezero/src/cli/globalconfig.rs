use clap::{Args, Subcommand};

use doublezero_cli::globalconfig::{get::*, set::*};

use doublezero_cli::allowlist::foundation::{
    add::AddFoundationAllowlistCliCommand, list::ListFoundationAllowlistCliCommand,
    remove::RemoveFoundationAllowlistCliCommand,
};

#[derive(Args, Debug)]
pub struct GlobalConfigCliCommand {
    #[command(subcommand)]
    pub command: GlobalConfigCommands,
}

#[derive(Debug, Subcommand)]
pub enum GlobalConfigCommands {
    Get(GetGlobalConfigCliCommand),
    Set(SetGlobalConfigCliCommand),
    Allowlist(FoundationAllowlistCliCommand),
}

#[derive(Args, Debug)]
pub struct FoundationAllowlistCliCommand {
    #[command(subcommand)]
    pub command: FoundationAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum FoundationAllowlistCommands {
    List(ListFoundationAllowlistCliCommand),
    Add(AddFoundationAllowlistCliCommand),
    Remove(RemoveFoundationAllowlistCliCommand),
}
