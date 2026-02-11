use clap::{Args, Subcommand};
use doublezero_cli::accesspass::{
    close::CloseAccessPassCliCommand, get::GetAccessPassCliCommand, list::ListAccessPassCliCommand,
    set::SetAccessPassCliCommand,
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
    /// Get access pass details
    #[clap()]
    Get(GetAccessPassCliCommand),
}
