use crate::command::init::InitArgs;
use crate::command::globalconfig::GlobalConfigArgs;

use double_zero_sdk::cli::keygen::KeyGenArgs;
use double_zero_sdk::cli::address::AddressArgs;
use double_zero_sdk::cli::balance::BalanceArgs;
use double_zero_sdk::cli::account::GetAccountArgs;
use crate::command::device::DeviceArgs;
use crate::command::exchange::ExchangeArgs;
use crate::command::location::LocationArgs;
use crate::command::tunnel::TunnelArgs;
use crate::command::user::UserArgs;
use crate::command::export::ExportArgs;

use clap::Subcommand;
use log::LogArgs;

pub mod globalconfig;
pub mod device;
pub mod exchange;
pub mod export;
pub mod init;
pub mod location;
pub mod tunnel;
pub mod user;
pub mod log;

use double_zero_sdk::ConfigArgs;

#[derive(Subcommand, Debug)]
pub enum Command {
    #[command(about = "", hide = true)] 
    Init(InitArgs),
    #[command(about = "Connect your server to a doublezero device", hide = false)]
    Address(AddressArgs),
    #[command(about = "Get your balance", hide = false)]
    Balance(BalanceArgs),
    #[command(about = "local configuration", hide = false)]
    Config(ConfigArgs),
    #[command(about = "Global network configuration", hide = false)]
    GlobalConfig(GlobalConfigArgs),
    #[command(about = "Get Account", hide = false)]
    Account(GetAccountArgs),
    #[command(about = "Manage locations", hide = false)]
    Location(LocationArgs),
    #[command(about = "Manage exchanges", hide = false)]
    Exchange(ExchangeArgs),
    #[command(about = "Manage devices", hide = false)]
    Device(DeviceArgs),
    #[command(about = "Manage tunnels between devices", hide = false)]
    Tunnel(TunnelArgs),
    #[command(about = "Manage users", hide = false)]
    User(UserArgs),
    #[command(about = "Export all data to files", hide = false)]
    Export(ExportArgs),
    #[command(about = "Create a new user identity", hide = false)]
    Keygen(KeyGenArgs),
    #[command(about = "Get logs", hide = false)]
    Log(LogArgs),
}
