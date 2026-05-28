pub mod add_target;
pub mod create;
pub mod delete;
pub mod get;
pub mod list;
pub mod remove_target;
pub mod set_result_destination;
pub mod update;
pub mod update_payment_status;

use clap::{Args, Subcommand};

use add_target::AddTargetCliCommand;
use create::CreateGeolocationUserCliCommand;
use delete::DeleteGeolocationUserCliCommand;
use get::GetGeolocationUserCliCommand;
use list::ListGeolocationUserCliCommand;
use remove_target::RemoveTargetCliCommand;
use set_result_destination::SetResultDestinationCliCommand;
use update::UpdateGeolocationUserCliCommand;
use update_payment_status::UpdatePaymentStatusCliCommand;

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
