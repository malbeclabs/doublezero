use clap::{Args, Subcommand};

use doublezero_cli::tenant::{
    add_administrator::*, create::*, delete::*, get::*, list::*, remove_administrator::*, update::*,
};

#[derive(Args, Debug)]
pub struct TenantCliCommand {
    #[command(subcommand)]
    pub command: TenantCommands,
}

#[derive(Debug, Subcommand)]
pub enum TenantCommands {
    /// Create a new tenant
    #[clap()]
    Create(CreateTenantCliCommand),
    /// Update an existing tenant
    #[clap()]
    Update(UpdateTenantCliCommand),
    /// List all tenants
    #[clap()]
    List(ListTenantCliCommand),
    /// Get details for a specific tenant
    #[clap()]
    Get(GetTenantCliCommand),
    /// Delete a tenant
    #[clap()]
    Delete(DeleteTenantCliCommand),
    /// Manage tenant administrators
    #[clap()]
    Administrator(AdministratorCliCommand),
}

#[derive(Args, Debug)]
pub struct AdministratorCliCommand {
    #[command(subcommand)]
    pub command: AdministratorCommands,
}

#[derive(Debug, Subcommand)]
pub enum AdministratorCommands {
    /// Add an administrator to a tenant
    #[clap()]
    Add(AddAdministratorTenantCliCommand),
    /// Remove an administrator from a tenant
    #[clap()]
    Remove(RemoveAdministratorTenantCliCommand),
}
