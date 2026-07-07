use clap::{Args, Subcommand};

use crate::permission::{audit::*, delete::*, get::*, list::*, resume::*, set::*, suspend::*};

#[derive(Args, Debug)]
pub struct PermissionCliCommand {
    #[command(subcommand)]
    pub command: PermissionCommands,
}

#[derive(Debug, Subcommand)]
pub enum PermissionCommands {
    /// Create or update a permission account for a pubkey
    #[clap()]
    Set(SetPermissionCliCommand),
    /// Suspend a permission (disables authorization)
    #[clap()]
    Suspend(SuspendPermissionCliCommand),
    /// Resume a suspended permission
    #[clap()]
    Resume(ResumePermissionCliCommand),
    /// Delete a permission account
    #[clap()]
    Delete(DeletePermissionCliCommand),
    /// Get details for a permission account
    #[clap()]
    Get(GetPermissionCliCommand),
    /// List all permission accounts
    #[clap()]
    List(ListPermissionCliCommand),
    /// Audit legacy→Permission parity before enabling require-permission-accounts
    #[clap()]
    Audit(AuditPermissionCliCommand),
}
