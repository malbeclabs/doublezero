use clap::{Args, Subcommand};

use doublezero_cli::{
    allowlist::user::{
        add::AddUserAllowlistCliCommand, list::ListUserAllowlistCliCommand,
        remove::RemoveUserAllowlistCliCommand,
    },
    user::{create::*, delete::*, get::*, list::*, request_ban::*, update::*},
};

#[derive(Args, Debug)]
pub struct UserCliCommand {
    #[command(subcommand)]
    pub command: UserCommands,
}

#[derive(Debug, Subcommand)]
pub enum UserCommands {
    Create(CreateUserCliCommand),
    Update(UpdateUserCliCommand),
    List(ListUserCliCommand),
    Get(GetUserCliCommand),
    Delete(DeleteUserCliCommand),
    #[command(about = "allowlist", hide = false)]
    Allowlist(UserAllowlistCliCommand),
    RequestBan(RequestBanUserCliCommand),
}

#[derive(Args, Debug)]
pub struct UserAllowlistCliCommand {
    #[command(subcommand)]
    pub command: UserAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum UserAllowlistCommands {
    List(ListUserAllowlistCliCommand),
    Add(AddUserAllowlistCliCommand),
    Remove(RemoveUserAllowlistCliCommand),
}
