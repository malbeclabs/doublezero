use clap::{Args, Subcommand};
use doublezero_cli::geolocation::user::{
    add_target::AddTargetCliCommand, create::CreateGeolocationUserCliCommand,
    delete::DeleteGeolocationUserCliCommand, get::GetGeolocationUserCliCommand,
    list::ListGeolocationUserCliCommand, remove_target::RemoveTargetCliCommand,
    update_payment_status::UpdatePaymentStatusCliCommand,
};

#[derive(Args, Debug)]
pub struct UserCliCommand {
    #[command(subcommand)]
    pub command: UserCommands,
}

#[derive(Subcommand, Debug)]
pub enum UserCommands {
    /// Create a new geolocation user
    Create(CreateGeolocationUserCliCommand),
    /// Delete a geolocation user
    Delete(DeleteGeolocationUserCliCommand),
    /// Get details of a specific user
    Get(GetGeolocationUserCliCommand),
    /// List all geolocation users
    List(ListGeolocationUserCliCommand),
    /// Add a target to a user
    AddTarget(AddTargetCliCommand),
    /// Remove a target from a user
    RemoveTarget(RemoveTargetCliCommand),
    /// Update payment status (foundation-only)
    UpdatePayment(UpdatePaymentStatusCliCommand),
}
