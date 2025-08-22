use clap::{Args, Subcommand};
use doublezero_cli::{
    allowlist::foundation::{
        add::AddFoundationAllowlistCliCommand, list::ListFoundationAllowlistCliCommand,
        remove::RemoveFoundationAllowlistCliCommand,
    },
    globalconfig::{
        airdrop::{get::GetAirdropCliCommand, set::SetAirdropCliCommand},
        authority::{get::GetAuthorityCliCommand, set::SetAuthorityCliCommand},
        get::GetGlobalConfigCliCommand,
        set::SetGlobalConfigCliCommand,
    },
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
    /// Set the global configuration airdrops
    #[clap()]
    Airdrop(AirdropCommand),
    /// Set the global configuration authority
    #[clap()]
    Authority(AuthorityCommand),
    /// Manage the foundation allowlist
    #[clap()]
    Allowlist(FoundationAllowlistCliCommand),
}

#[derive(Args, Debug)]
pub struct AuthorityCommand {
    #[command(subcommand)]
    pub command: AuthorityCommands,
}

#[derive(Debug, Subcommand)]
pub enum AuthorityCommands {
    /// Set the global configuration authority
    #[clap()]
    Set(SetAuthorityCliCommand),
    /// Get the global configuration authority
    #[clap()]
    Get(GetAuthorityCliCommand),
}

#[derive(Args, Debug)]
pub struct AirdropCommand {
    #[command(subcommand)]
    pub command: AirdropCommands,
}

#[derive(Debug, Subcommand)]
pub enum AirdropCommands {
    /// Set the global configuration airdrops
    #[clap()]
    Set(SetAirdropCliCommand),
    /// Get the global configuration airdrops
    #[clap()]
    Get(GetAirdropCliCommand),
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
