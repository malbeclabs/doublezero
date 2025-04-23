use clap::Subcommand;

use double_zero_cli::init::InitArgs;

use double_zero_cli::keygen::KeyGenArgs;
use double_zero_cli::account::GetAccountArgs;
use double_zero_cli::address::AddressArgs;
use double_zero_cli::balance::BalanceArgs;
use double_zero_cli::export::ExportArgs;
use double_zero_cli::latency::LatencyArgs;
use double_zero_cli::status::StatusArgs;
use double_zero_cli::connect::ProvisioningArgs;
use double_zero_cli::disconnect::DecommissioningArgs;
use double_zero_cli::log::LogArgs;

use crate::config::ConfigArgs;
use crate::device::DeviceArgs;
use crate::exchange::ExchangeArgs;
use crate::globalconfig::GlobalConfigArgs;
use crate::location::LocationArgs;
use crate::tunnel::TunnelArgs;
use crate::user::UserArgs;


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
