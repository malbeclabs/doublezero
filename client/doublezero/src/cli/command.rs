use clap::Subcommand;

use doublezero_cli::init::InitArgs;

use doublezero_cli::account::GetAccountArgs;
use doublezero_cli::address::AddressArgs;
use doublezero_cli::balance::BalanceArgs;
use doublezero_cli::export::ExportArgs;
use doublezero_cli::keygen::KeyGenArgs;
use doublezero_cli::log::LogArgs;

use crate::cli::globalconfig::GlobalConfigArgs;
use crate::cli::config::ConfigArgs;
use crate::cli::device::DeviceArgs;
use crate::cli::exchange::ExchangeArgs;
use crate::cli::location::LocationArgs;
use crate::cli::tunnel::TunnelArgs;
use crate::cli::user::UserArgs;

use crate::command::connect::ProvisioningArgs;
use crate::command::disconnect::DecommissioningArgs;
use crate::command::status::StatusArgs;
use crate::command::latency::LatencyArgs;

#[derive(Subcommand, Debug)]
pub enum Command {
    #[command(about = "", hide = true)]
    Init(InitArgs),
    #[command(about = "Connect your server to a doublezero device", hide = false)]
    Connect(ProvisioningArgs),
    #[command(about = "Get the status of your service", hide = false)]
    Status(StatusArgs),
    #[command(
        about = "Disconnect your server from the doublezero network",
        hide = false
    )]
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
