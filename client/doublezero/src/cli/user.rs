use clap::Args;
use clap::Subcommand;

use doublezero_cli::allowlist::user::add::AddUserAllowlistCliCommand;
use doublezero_cli::allowlist::user::list::ListUserAllowlistCliCommand;
use doublezero_cli::allowlist::user::remove::RemoveUserAllowlistCliCommand;
use doublezero_cli::user::create::CreateUserCliCommand;
use doublezero_cli::user::create_subscribe::CreateSubscribeUserCliCommand;
use doublezero_cli::user::delete::DeleteUserCliCommand;
use doublezero_cli::user::get::GetUserCliCommand;
use doublezero_cli::user::list::ListUserCliCommand;
use doublezero_cli::user::request_ban::RequestBanUserCliCommand;
use doublezero_cli::user::update::UpdateUserCliCommand;

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
