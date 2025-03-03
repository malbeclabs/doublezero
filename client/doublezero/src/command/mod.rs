use crate::command::init::InitArgs;
use crate::command::globalconfig::GlobalConfigArgs;

use crate::command::keygen::KeyGenArgs;
use crate::command::getaccount::GetAccountArgs;
use crate::command::address::AddressArgs;
use crate::command::balance::BalanceArgs;
use crate::command::exchange::ExchangeArgs;
use crate::command::location::LocationArgs;
use crate::command::tunnel::TunnelArgs;
use crate::command::user::UserArgs;
use crate::command::export::ExportArgs;
use crate::command::latency::LatencyArgs;
use crate::command::connect::ProvisioningArgs;
use crate::DeviceArgs;

use clap::Subcommand;
use config::ConfigArgs;
use disconnect::DecommissioningArgs;
use log::LogArgs;
use status::StatusArgs;

pub mod address;
pub mod keygen;
pub mod balance;
pub mod config;
pub mod globalconfig;
pub mod getaccount;
pub mod device;
pub mod exchange;
pub mod export;
pub mod init;
pub mod latency;
pub mod location;
pub mod tunnel;
pub mod user;
pub mod connect;
pub mod disconnect;
pub mod log;
pub mod status;
pub mod helpers;

#[derive(Subcommand, Debug)]
pub enum Command {
    #[command(about = "", hide = true)] 
    Init(InitArgs),
    #[command(about = "Connect your server to a doublezero device", hide = false)]
    Connect(ProvisioningArgs),
    #[command(about = "Get the status of your service", hide = false)]
    Status(StatusArgs),
    #[command(about = "Disconnect your server from the doublezero network", hide = false)]
    Disconnect(DecommissioningArgs),
    #[command(about = "Get device latencies", hide = false)]
    Latency(LatencyArgs),
    #[command(about = "Get your public key", hide = false)]
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
