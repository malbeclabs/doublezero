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
    /// Get the current global configuration
    #[clap()]
    Get(GetGlobalConfigCliCommand),
    /// Set the global configuration
    #[clap()]
    Set(SetGlobalConfigCliCommand),
    /// Manage the foundation allowlist
    #[clap()]
    Allowlist(FoundationAllowlistCliCommand),
}

#[derive(Args, Debug)]
pub struct FoundationAllowlistCliCommand {
    #[command(subcommand)]
    pub command: FoundationAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum FoundationAllowlistCommands {
    /// List foundation allowlist
    #[clap()]
    List(ListFoundationAllowlistCliCommand),
    /// Add a foundation to the allowlist
    #[clap()]
    Add(AddFoundationAllowlistCliCommand),
    /// Remove a foundation from the allowlist
    #[clap()]
    Remove(RemoveFoundationAllowlistCliCommand),
}
