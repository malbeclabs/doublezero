use clap::Args;
use clap::Subcommand;

use double_zero_cli::user::create::*;
use double_zero_cli::user::update::*;
use double_zero_cli::user::list::*;
use double_zero_cli::user::get::*;
use double_zero_cli::user::delete::*;
use double_zero_cli::user::request_ban::*;
use double_zero_cli::user::allowlist::get::GetAllowlistArgs;
use double_zero_cli::user::allowlist::add::*;
use double_zero_cli::user::allowlist::remove::*;

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
    Allowlist(AllowlistArgs),
    RequestBan(RequestBanUserArgs),    
}


#[derive(Args, Debug)]
pub struct AllowlistArgs {
    #[command(subcommand)]
    pub command: AllowlistCommands,
}

#[derive(Debug, Subcommand)]
pub enum AllowlistCommands {
    Get(GetAllowlistArgs),
    Add(AddAllowlistArgs),
    Remove(RemoveAllowlistArgs),
}