use clap::Subcommand;

use doublezero_cli::init::InitArgs;

use doublezero_cli::keygen::KeyGenArgs;
use doublezero_cli::account::GetAccountArgs;
use doublezero_cli::address::AddressArgs;
use doublezero_cli::balance::BalanceArgs;
use doublezero_cli::export::ExportArgs;
use doublezero_cli::log::LogArgs;

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
