use clap::{Args, Subcommand};

use crate::user::{
    create::CreateUserCliCommand, create_subscribe::CreateSubscribeUserCliCommand,
    delete::DeleteUserCliCommand, get::GetUserCliCommand, list::ListUserCliCommand,
    recreate::RecreateUserCliCommand, request_ban::RequestBanUserCliCommand,
    subscribe::SubscribeUserCliCommand, update::UpdateUserCliCommand,
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
    /// Delete and recreate a user in one transaction (not on mainnet-beta)
    #[command(hide = true)]
    Recreate(RecreateUserCliCommand),
    /// Request a ban for a user
    #[command(hide = true)]
    RequestBan(RequestBanUserCliCommand),
}
