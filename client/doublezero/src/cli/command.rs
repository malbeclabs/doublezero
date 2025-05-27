use super::multicast::MulticastCliCommand;
use crate::cli::config::ConfigCliCommand;
use crate::cli::device::DeviceCliCommand;
use crate::cli::exchange::ExchangeCliCommand;
use crate::cli::globalconfig::GlobalConfigCliCommand;
use crate::cli::location::LocationCliCommand;
use crate::cli::tunnel::TunnelCliCommand;
use crate::cli::user::UserCliCommand;
use crate::command::connect::ProvisioningCliCommand;
use crate::command::disconnect::DecommissioningCliCommand;
use crate::command::latency::LatencyCliCommand;
use crate::command::status::StatusCliCommand;
use clap::Args;
use clap::Subcommand;
use clap_complete::Shell;
use doublezero_cli::account::GetAccountCliCommand;
use doublezero_cli::address::AddressCliCommand;
use doublezero_cli::balance::BalanceCliCommand;
use doublezero_cli::export::ExportCliCommand;
use doublezero_cli::init::InitCliCommand;
use doublezero_cli::keygen::KeyGenCliCommand;
use doublezero_cli::log::LogCliCommand;

#[derive(Subcommand, Debug)]
pub enum Command {
    #[command(about = "", hide = true)]
    Init(InitCliCommand),
    #[command(about = "Connect your server to a doublezero device", hide = false)]
    Connect(ProvisioningCliCommand),
    #[command(about = "Get the status of your service", hide = false)]
    Status(StatusCliCommand),
    #[command(
        about = "Disconnect your server from the doublezero network",
        hide = false
    )]
    Disconnect(DecommissioningCliCommand),
    #[command(about = "Get device latencies", hide = false)]
    Latency(LatencyCliCommand),
    #[command(about = "Get your public key", hide = false)]
    Address(AddressCliCommand),
    #[command(about = "Get your balance", hide = false)]
    Balance(BalanceCliCommand),
    #[command(about = "local configuration", hide = false)]
    Config(ConfigCliCommand),
    #[command(about = "Global network configuration", hide = false)]
    GlobalConfig(GlobalConfigCliCommand),
    #[command(about = "Get Account", hide = false)]
    Account(GetAccountCliCommand),
    #[command(about = "Manage locations", hide = false)]
    Location(LocationCliCommand),
    #[command(about = "Manage exchanges", hide = false)]
    Exchange(ExchangeCliCommand),
    #[command(about = "Manage devices", hide = false)]
    Device(DeviceCliCommand),
    #[command(about = "Manage tunnels between devices", hide = false)]
    Tunnel(TunnelCliCommand),
    #[command(about = "Manage users", hide = false)]
    User(UserCliCommand),
    #[command(about = "Manage multicast", hide = false)]
    Multicast(MulticastCliCommand),
    #[command(about = "Export all data to files", hide = false)]
    Export(ExportCliCommand),
    #[command(about = "Create a new user identity", hide = false)]
    Keygen(KeyGenCliCommand),
    #[command(about = "Get logs", hide = false)]
    Log(LogCliCommand),
    #[command(about = "Generate shell completions", hide = false)]
    Completion(CompletionCliCommand),
}

#[derive(Args, Debug, Clone)]
pub struct CompletionCliCommand {
    #[arg(value_enum)]
    pub shell: Shell,
}
