use clap::{Args, Subcommand};
use doublezero_cli::resource::{
    allocate::AllocateResourceCliCommand, create::CreateResourceCliCommand,
    deallocate::DeallocateResourceCliCommand, get::GetResourceCliCommand,
};

#[derive(Args, Debug)]
pub struct ResourceCliCommand {
    #[command(subcommand)]
    pub command: ResourceCommands,
}

#[derive(Debug, Subcommand)]
pub enum ResourceCommands {
    /// Allocate a resource
    #[clap()]
    Allocate(AllocateResourceCliCommand),
    /// Create a resource
    #[clap()]
    Create(CreateResourceCliCommand),
    /// Deallocate a resource
    #[clap()]
    Deallocate(DeallocateResourceCliCommand),
    /// Get a resource
    #[clap()]
    Get(GetResourceCliCommand),
}
