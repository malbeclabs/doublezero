use super::multicast::MulticastCliCommand;
use crate::{
    cli::{
        accesspass::AccessPassCliCommand, config::ConfigCliCommand,
        contributor::ContributorCliCommand, device::DeviceCliCommand, exchange::ExchangeCliCommand,
        globalconfig::GlobalConfigCliCommand, link::LinkCliCommand, location::LocationCliCommand,
        user::UserCliCommand,
    },
    command::{
        connect::ProvisioningCliCommand, disconnect::DecommissioningCliCommand,
        latency::LatencyCliCommand, status::StatusCliCommand,
    },
};
use clap::{Args, Subcommand};
use clap_complete::Shell;
use doublezero_cli::{
    account::GetAccountCliCommand, accounts::GetAccountsCliCommand, address::AddressCliCommand,
    balance::BalanceCliCommand, export::ExportCliCommand, init::InitCliCommand,
    keygen::KeyGenCliCommand, logcommand::LogCliCommand,
};

#[derive(Subcommand, Debug)]
pub enum Command {
    #[command(hide = true)]
    Init(InitCliCommand),
    /// Connect your server to a doublezero device
    #[command()]
    Connect(ProvisioningCliCommand),
    /// Get the status of your service
    #[command()]
    Status(StatusCliCommand),
    /// Disconnect your server from the doublezero network
    #[command()]
    Disconnect(DecommissioningCliCommand),
    /// Get device latencies
    #[command()]
    Latency(LatencyCliCommand),
    /// Get your public key
    #[command()]
    Address(AddressCliCommand),
    /// Get your balance
    #[command()]
    Balance(BalanceCliCommand),
    /// local configuration
    #[command()]
    Config(ConfigCliCommand),
    /// Global network configuration
    #[command()]
    GlobalConfig(GlobalConfigCliCommand),
    /// Get Account
    #[command()]
    Account(GetAccountCliCommand),
    /// List Accounts
    #[command(hide = true)]
    Accounts(GetAccountsCliCommand),
    /// Manage locations
    #[command()]
    Location(LocationCliCommand),
    /// Manage exchanges
    #[command()]
    Exchange(ExchangeCliCommand),
    /// Manage contributors
    #[command()]
    Contributor(ContributorCliCommand),
    /// Manage devices
    #[command()]
    Device(DeviceCliCommand),
    /// Manage tunnels between devices
    #[command()]
    Link(LinkCliCommand),

    #[command()]
    AccessPass(AccessPassCliCommand),

    /// Manage users
    #[command()]
    User(UserCliCommand),
    /// Manage multicast
    #[command()]
    Multicast(MulticastCliCommand),
    /// Export all data to files
    #[command()]
    Export(ExportCliCommand),
    /// Create a new user identity
    #[command()]
    Keygen(KeyGenCliCommand),
    /// Get logs
    #[command()]
    Log(LogCliCommand),
    /// Generate shell completions
    #[command()]
    Completion(CompletionCliCommand),
}

#[derive(Args, Debug, Clone)]
pub struct CompletionCliCommand {
    #[arg(value_enum)]
    pub shell: Shell,
}
