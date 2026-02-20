use clap::{Args, Subcommand};
use doublezero_cli::{
    allowlist::{
        foundation::{
            add::AddFoundationAllowlistCliCommand, list::ListFoundationAllowlistCliCommand,
            remove::RemoveFoundationAllowlistCliCommand,
        },
        qa::{add::AddQaCliCommand, list::ListQaCliCommand, remove::RemoveQaCliCommand},
    },
    globalconfig::{
        airdrop::{get::GetAirdropCliCommand, set::SetAirdropCliCommand},
        authority::{get::GetAuthorityCliCommand, set::SetAuthorityCliCommand},
        featureflags::{get::GetFeatureFlagsCliCommand, set::SetFeatureFlagsCliCommand},
        get::GetGlobalConfigCliCommand,
        set::SetGlobalConfigCliCommand,
        setversion::SetVersionCliCommand,
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
    /// Set the global configuration authority
    #[clap()]
    Authority(AuthorityCommand),
    /// Set the global configuration airdrop values
    #[clap()]
    Airdrop(AirdropCommand),
    /// Manage the foundation allowlist
    #[clap()]
    Allowlist(FoundationAllowlistCliCommand),
    /// Manage the QA allowlist
    #[clap()]
    QaAllowlist(QaAllowlistCliCommand),
    /// Set the minimum compatible client version
    #[clap(hide = true)]
    SetVersion(SetVersionCliCommand),
    /// Manage feature flags
    #[clap(hide = true)]
    FeatureFlags(FeatureFlagsCommand),
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

#[derive(Args, Debug)]
pub struct QaAllowlistCliCommand {
    #[command(subcommand)]
    pub command: QaAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum QaAllowlistCommands {
    /// List QA allowlist
    #[clap()]
    List(ListQaCliCommand),
    /// Add a pubkey to the QA allowlist
    #[clap()]
    Add(AddQaCliCommand),
    /// Remove a pubkey from the QA allowlist
    #[clap()]
    Remove(RemoveQaCliCommand),
}

#[derive(Args, Debug)]
pub struct FeatureFlagsCommand {
    #[command(subcommand)]
    pub command: FeatureFlagsCommands,
}

#[derive(Debug, Subcommand)]
pub enum FeatureFlagsCommands {
    /// Get the current feature flags
    #[clap()]
    Get(GetFeatureFlagsCliCommand),
    /// Set feature flags
    #[clap()]
    Set(SetFeatureFlagsCliCommand),
}
