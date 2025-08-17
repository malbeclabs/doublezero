use clap::{Args, Subcommand};
use doublezero_cli::accesspass::{list::ListAccessPassCliCommand, set::SetAccessPassCliCommand};

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
    #[clap()]
    List(ListAccessPassCliCommand),
}
