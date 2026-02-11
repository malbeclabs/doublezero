use clap::{Args, Subcommand};

use doublezero_cli::user::{
    create::CreateUserCliCommand, create_subscribe::CreateSubscribeUserCliCommand,
    delete::DeleteUserCliCommand, get::GetUserCliCommand, list::ListUserCliCommand,
    request_ban::RequestBanUserCliCommand, subscribe::SubscribeUserCliCommand,
    update::UpdateUserCliCommand,
};

#[derive(Args, Debug)]
pub struct UserCliCommand {
    #[command(subcommand)]
    pub command: UserCommands,
}

#[derive(Debug, Subcommand)]
pub enum UserCommands {
    /// Create a new user
    #[command(hide = true)]
    Create(CreateUserCliCommand),
    /// Create and subscribe a new user
    #[command(hide = true)]
    CreateSubscribe(CreateSubscribeUserCliCommand),
    /// Subscribe an existing user
    #[command(hide = true)]
    Subscribe(SubscribeUserCliCommand),
    /// Update an existing user
    #[command(hide = true)]
    Update(UpdateUserCliCommand),
    /// List all users
    #[command()]
    List(Box<ListUserCliCommand>),
    /// Get details for a specific user
    #[command()]
    Get(GetUserCliCommand),
    /// Delete a user
    #[command(hide = true)]
    Delete(DeleteUserCliCommand),
    /// Request a ban for a user
    #[command(hide = true)]
    RequestBan(RequestBanUserCliCommand),
}
