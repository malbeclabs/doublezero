use clap::{Args, Subcommand};

use doublezero_cli::{
    allowlist::user::{
        add::AddUserAllowlistCliCommand, list::ListUserAllowlistCliCommand,
        remove::RemoveUserAllowlistCliCommand,
    },
    user::{
        create::CreateUserCliCommand, create_subscribe::CreateSubscribeUserCliCommand,
        delete::DeleteUserCliCommand, get::GetUserCliCommand, list::ListUserCliCommand,
        request_ban::RequestBanUserCliCommand, subscribe::SubscribeUserCliCommand,
        update::UpdateUserCliCommand,
    },
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
    List(ListUserCliCommand),
    /// Get details for a specific user
    #[command()]
    Get(GetUserCliCommand),
    /// Delete a user
    #[command(hide = true)]
    Delete(DeleteUserCliCommand),
    /// Manage user allowlist
    #[command()]
    Allowlist(UserAllowlistCliCommand),
    /// Request a ban for a user
    #[command(hide = true)]
    RequestBan(RequestBanUserCliCommand),
}

#[derive(Args, Debug)]
pub struct UserAllowlistCliCommand {
    #[command(subcommand)]
    pub command: UserAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum UserAllowlistCommands {
    /// List user allowlist
    #[clap()]
    List(ListUserAllowlistCliCommand),
    /// Add a user to the allowlist
    #[clap()]
    Add(AddUserAllowlistCliCommand),
    /// Remove a user from the allowlist
    #[clap()]
    Remove(RemoveUserAllowlistCliCommand),
}
