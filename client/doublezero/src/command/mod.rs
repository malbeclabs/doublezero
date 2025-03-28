use crate::command::init::InitArgs;
use double_zero_sdk::cli::keygen::KeyGenArgs;
use double_zero_sdk::cli::address::AddressArgs;
use double_zero_sdk::cli::balance::BalanceArgs;
use double_zero_sdk::cli::account::GetAccountArgs;
use double_zero_sdk::ConfigArgs;


use crate::command::devices::ListDeviceArgs;
use crate::command::export::ExportArgs;
use crate::command::latency::LatencyArgs;
use crate::command::connect::ProvisioningArgs;

use clap::Subcommand;
use disconnect::DecommissioningArgs;
use log::LogArgs;
use status::StatusArgs;

pub mod export;
pub mod init;
pub mod latency;
pub mod connect;
pub mod disconnect;
pub mod devices;
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
    #[command(about = "List Devices", hide = false)]
    Devices(ListDeviceArgs),


    #[command(about = "Get your public key", hide = false)]
    Address(AddressArgs),
    #[command(about = "Get your balance", hide = false)]
    Balance(BalanceArgs),
    #[command(about = "local configuration", hide = false)]
    Config(ConfigArgs),
    #[command(about = "Get Account", hide = false)]
    Account(GetAccountArgs),
    #[command(about = "Export all data to files", hide = false)]
    Export(ExportArgs),
    #[command(about = "Create a new user identity", hide = false)]
    Keygen(KeyGenArgs),
    #[command(about = "Get logs", hide = false)]
    Log(LogArgs),
}
