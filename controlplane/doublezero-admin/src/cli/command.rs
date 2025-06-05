use clap::Subcommand;

use doublezero_cli::init::InitCliCommand;

use doublezero_cli::{
    account::GetAccountCliCommand, address::AddressCliCommand, balance::BalanceCliCommand,
    export::ExportCliCommand, keygen::KeyGenCliCommand, log::LogCliCommand,
};

use crate::cli::{
    config::ConfigCliCommand, device::DeviceCliCommand, exchange::ExchangeCliCommand,
    globalconfig::GlobalConfigCliCommand, link::LinkCliCommand, location::LocationCliCommand,
    user::UserCliCommand,
};

#[derive(Subcommand, Debug)]
pub enum Command {
    #[command(about = "", hide = true)]
    Init(InitCliCommand),
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
    Link(LinkCliCommand),
    #[command(about = "Manage users", hide = false)]
    User(UserCliCommand),
    #[command(about = "Export all data to files", hide = false)]
    Export(ExportCliCommand),
    #[command(about = "Create a new user identity", hide = false)]
    Keygen(KeyGenCliCommand),
    #[command(about = "Get logs", hide = false)]
    Log(LogCliCommand),
}
