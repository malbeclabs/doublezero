use super::multicast::MulticastCliCommand;
use crate::{
    cli::{
        config::ConfigCliCommand, contributor::ContributorCliCommand, device::DeviceCliCommand,
        exchange::ExchangeCliCommand, globalconfig::GlobalConfigCliCommand, link::LinkCliCommand,
        location::LocationCliCommand, user::UserCliCommand,
    },
    command::{
        connect::ProvisioningCliCommand, disconnect::DecommissioningCliCommand,
        latency::LatencyCliCommand, status::StatusCliCommand,
    },
};
use clap::{Args, Subcommand};
use clap_complete::Shell;
use doublezero_cli::{
    account::GetAccountCliCommand, address::AddressCliCommand, balance::BalanceCliCommand,
    export::ExportCliCommand, init::InitCliCommand, keygen::KeyGenCliCommand, log::LogCliCommand,
};

#[derive(Subcommand, Debug)]
pub enum Command {
    #[command(hide = true)]
    Init(InitCliCommand),
    /// Connect your server to a doublezero device
    #[command(hide = false)]
    Connect(ProvisioningCliCommand),
    /// Get the status of your service
    #[command(hide = false)]
    Status(StatusCliCommand),
    /// Disconnect your server from the doublezero network
    #[command(hide = false)]
    Disconnect(DecommissioningCliCommand),
    /// Get device latencies
    #[command(hide = false)]
    Latency(LatencyCliCommand),
    /// Get your public key
    #[command(hide = false)]
    Address(AddressCliCommand),
    /// Get your balance
    #[command(hide = false)]
    Balance(BalanceCliCommand),
    /// local configuration
    #[command(hide = false)]
    Config(ConfigCliCommand),
    /// Global network configuration
    #[command(hide = false)]
    GlobalConfig(GlobalConfigCliCommand),
    /// Get Account
    #[command(hide = false)]
    Account(GetAccountCliCommand),
    /// Manage locations
    #[command(hide = false)]
    Location(LocationCliCommand),
    /// Manage exchanges
    #[command(hide = false)]
    Exchange(ExchangeCliCommand),
    /// Manage contributors
    #[command(hide = false)]
    Contributor(ContributorCliCommand),
    #[command(hide = false)]
    Device(DeviceCliCommand),
    /// Manage tunnels between devices
    #[command(hide = false)]
    Link(LinkCliCommand),
    /// Manage users
    #[command(hide = false)]
    User(UserCliCommand),
    /// Manage multicast
    #[command(hide = false)]
    Multicast(MulticastCliCommand),
    /// Export all data to files
    #[command(hide = false)]
    Export(ExportCliCommand),
    /// Create a new user identity
    #[command(hide = false)]
    Keygen(KeyGenCliCommand),
    /// Get logs
    #[command(hide = false)]
    Log(LogCliCommand),
    /// Generate shell completions
    #[command(hide = false)]
    Completion(CompletionCliCommand),
}

#[derive(Args, Debug, Clone)]
pub struct CompletionCliCommand {
    #[arg(value_enum)]
    pub shell: Shell,
}
