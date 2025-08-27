use clap::{Args, Subcommand};
use doublezero_cli::accesspass::{
    close::CloseAccessPassCliCommand, list::ListAccessPassCliCommand,
    prepaid::SetAccessPassPrepaidCliCommand,
    solana_validators::SetAccessPassSolanaValidatorCliCommand,
};

#[derive(Args, Debug)]
pub struct AccessPassCliCommand {
    #[command(subcommand)]
    pub command: AccessPassCommands,
}

#[derive(Debug, Subcommand)]
pub enum AccessPassCommands {
    /// Set access pass
    #[clap()]
    Set(SetAccessPassCliCommand),
    /// Close access pass
    #[clap()]
    Close(CloseAccessPassCliCommand),
    /// List access passes
    #[clap()]
    List(ListAccessPassCliCommand),
}

#[derive(Args, Debug)]
pub struct SetAccessPassCliCommand {
    #[command(subcommand)]
    pub command: SetAccessPassCliCommands,
}

#[derive(Debug, Subcommand)]
pub enum SetAccessPassCliCommands {
    /// Set access pass
    #[clap()]
    Prepaid(SetAccessPassPrepaidCliCommand),
    /// Set access pass for Solana validators
    #[clap()]
    SolanaValidator(SetAccessPassSolanaValidatorCliCommand),
}
