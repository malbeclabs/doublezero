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
    #[command(about = "", hide = true)]
    Create(CreateUserCliCommand),
    #[command(about = "", hide = true)]
    CreateSubscribe(CreateSubscribeUserCliCommand),
    #[command(about = "", hide = true)]
    Subscribe(SubscribeUserCliCommand),
    #[command(about = "", hide = true)]
    Update(UpdateUserCliCommand),
    List(ListUserCliCommand),
    Get(GetUserCliCommand),
    #[command(about = "", hide = true)]
    Delete(DeleteUserCliCommand),
    #[command(about = "allowlist", hide = false)]
    Allowlist(UserAllowlistCliCommand),
    #[command(about = "", hide = true)]
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
