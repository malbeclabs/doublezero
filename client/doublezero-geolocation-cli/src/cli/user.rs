use clap::{Args, Subcommand};
use doublezero_cli::geolocation::user::{
    add_target::AddTargetCliCommand, create::CreateGeolocationUserCliCommand,
    delete::DeleteGeolocationUserCliCommand, get::GetGeolocationUserCliCommand,
    list::ListGeolocationUserCliCommand, remove_target::RemoveTargetCliCommand,
    set_result_destination::SetResultDestinationCliCommand,
    update::UpdateGeolocationUserCliCommand, update_payment_status::UpdatePaymentStatusCliCommand,
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
    /// Update a geolocation user's payment token account
    Update(UpdateGeolocationUserCliCommand),
    /// Get details of a specific user
    Get(GetGeolocationUserCliCommand),
    /// List all geolocation users
    List(ListGeolocationUserCliCommand),
    /// Add a target to a user
    AddTarget(AddTargetCliCommand),
    /// Remove a target from a user
    RemoveTarget(RemoveTargetCliCommand),
    /// Set result destination for geolocation results
    SetResultDestination(SetResultDestinationCliCommand),
    /// Update payment status (foundation-only)
    UpdatePayment(UpdatePaymentStatusCliCommand),
}
