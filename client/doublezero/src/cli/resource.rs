use clap::{Args, Subcommand};
use doublezero_cli::resource::{
    allocate::AllocateResourceCliCommand, deallocate::DeallocateResourceCliCommand,
    get::GetResourceCliCommand,
};

#[derive(Args, Debug)]
pub struct ResourceCliCommand {
    #[command(subcommand)]
    pub command: ResourceCommands,
}

#[derive(Debug, Subcommand)]
pub enum ResourceCommands {
    /// Set access pass
    #[clap()]
    Allocate(AllocateResourceCliCommand),
    /// Close access pass
    #[clap()]
    Deallocate(DeallocateResourceCliCommand),
    /// List access passes
    #[clap()]
    Get(GetResourceCliCommand),
}
