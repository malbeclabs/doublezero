use clap::Args;
use clap::Subcommand;

use doublezero_cli::user::create::*;
use doublezero_cli::user::update::*;
use doublezero_cli::user::list::*;
use doublezero_cli::user::get::*;
use doublezero_cli::user::delete::*;
use doublezero_cli::user::request_ban::*;
use doublezero_cli::user::allowlist::get::GetAllowlistArgs;
use doublezero_cli::user::allowlist::add::*;
use doublezero_cli::user::allowlist::remove::*;

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