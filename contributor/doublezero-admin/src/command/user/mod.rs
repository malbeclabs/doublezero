use clap::Args;
use clap::Subcommand;
use request_ban::RequestBanUserArgs;

use self::create::*;
use self::update::*;
use self::list::*;
use self::get::*;
use self::delete::*;
use self::allowlist::*;

pub mod create;
pub mod update;
pub mod list;
pub mod get;
pub mod delete;
pub mod allowlist;
pub mod request_ban;


#[derive(Args, Debug)]
pub struct UserArgs {
    #[command(subcommand)]
    pub command: UserCommands,
}

#[derive(Debug, Subcommand)]
pub enum UserCommands {
    #[command(about = "", hide = true)] 
    Create(CreateUserArgs),
    Update(UpdateUserArgs),
    List(ListUserArgs),
    Get(GetUserArgs),
    #[command(about = "", hide = true)] 
    Delete(DeleteUserArgs),
    #[command(about = "allowlist", hide = false)]
    Allowlist(AllowlistArgs),
    RequestBan(RequestBanUserArgs),
}
