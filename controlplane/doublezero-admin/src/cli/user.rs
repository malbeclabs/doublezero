use clap::Args;
use clap::Subcommand;

use doublezero_cli::allowlist::user::add::AddUserAllowlistCliCommand;
use doublezero_cli::allowlist::user::list::ListUserAllowlistCliCommand;
use doublezero_cli::allowlist::user::remove::RemoveUserAllowlistCliCommand;
use doublezero_cli::user::create::*;
use doublezero_cli::user::delete::*;
use doublezero_cli::user::get::*;
use doublezero_cli::user::list::*;
use doublezero_cli::user::request_ban::*;
use doublezero_cli::user::update::*;

#[derive(Args, Debug)]
pub struct UserCliCommand {
    #[command(subcommand)]
    pub command: UserCommands,
}

#[derive(Debug, Subcommand)]
pub enum UserCommands {
    Create(CreateUserCliCommand),
    Update(UpdateUserCliCommand),cd 
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
