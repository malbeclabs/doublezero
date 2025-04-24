use clap::Args;
use clap::Subcommand;

use doublezero_cli::user::create::*;
use doublezero_cli::user::update::*;
use doublezero_cli::user::list::*;
use doublezero_cli::user::get::*;
use doublezero_cli::user::delete::*;
use doublezero_cli::user::request_ban::*;
use doublezero_cli::allowlist::user::list::ListUserAllowlistArgs;
use doublezero_cli::allowlist::user::add::AddUserAllowlistArgs;
use doublezero_cli::allowlist::user::remove::RemoveUserAllowlistArgs;

#[derive(Args, Debug)]
pub struct UserArgs {
    #[command(subcommand)]
    pub command: UserCommands,
}

#[derive(Debug, Subcommand)]
pub enum UserCommands {
    Create(CreateUserArgs),
    Update(UpdateUserArgs),
    List(ListUserArgs),
    Get(GetUserArgs),
    Delete(DeleteUserArgs),
    #[command(about = "allowlist", hide = false)]
    Allowlist(UserAllowlistArgs),
    RequestBan(RequestBanUserArgs),    
}


#[derive(Args, Debug)]
pub struct UserAllowlistArgs {
    #[command(subcommand)]
    pub command: UserAllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum UserAllowlistCommands {
    List(ListUserAllowlistArgs),
    Add(AddUserAllowlistArgs),
    Remove(RemoveUserAllowlistArgs),
}