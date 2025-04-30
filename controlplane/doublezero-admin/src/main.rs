use clap::Parser;
use colored::Colorize;

use doublezero_sdk::DZClient;

mod cli;
mod command;

use cli::command::Command;
use cli::config::ConfigCommands;
use cli::device::{DeviceAllowlistCommands, DeviceCommands};
use cli::exchange::ExchangeCommands;
use cli::globalconfig::{FoundationAllowlistCommands, GlobalConfigCommands};
use cli::location::LocationCommands;
use cli::tunnel::TunnelCommands;
use cli::user::{UserAllowlistCommands, UserCommands};

include!(concat!(env!("OUT_DIR"), "/version.rs"));

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "DoubleZeroAdmin")]
#[command(version = APP_VERSION)]
#[command(long_version = APP_LONG_VERSION)]
#[command(about = "Double Zero contributor tool", long_about = None)]
struct App {
    #[command(subcommand)]
    command: Command,

    #[arg(long, value_name = "RPC_URL", global = true)]
    url: Option<String>,
    #[arg(long, value_name = "WEBSOCKET_URL", global = true)]
    ws: Option<String>,
    #[arg(long, value_name = "PROGRAM_ID", global = true)]
    program_id: Option<String>,
    #[arg(long, value_name = "KEYPAIR", global = true)]
    keypair: Option<String>,
}

#[tokio::main]
async fn main() -> eyre::Result<()> {
    color_eyre::install()?;

    let app = App::parse();

    if let Some(keypair) = &app.keypair {
        println!("using keypair: {}", keypair);
    }

    let client = DZClient::new(app.url, app.ws, app.program_id, app.keypair)?;

    let res = match app.command {
        Command::Address(args) => args.execute(&client),
        Command::Balance(args) => args.execute(&client),
        Command::Reset(args) => args.execute(&client),

        Command::Init(args) => args.execute(&client),
        Command::Config(command) => match command.command {
            ConfigCommands::Get(args) => args.execute(&client),
            ConfigCommands::Set(args) => args.execute(&client),
        },
        Command::GlobalConfig(command) => match command.command {
            GlobalConfigCommands::Set(args) => args.execute(&client),
            GlobalConfigCommands::Get(args) => args.execute(&client),
            GlobalConfigCommands::Allowlist(command) => match command.command {
                FoundationAllowlistCommands::List(args) => args.execute(&client),
                FoundationAllowlistCommands::Add(args) => args.execute(&client),
                FoundationAllowlistCommands::Remove(args) => args.execute(&client),
            },
        },

        Command::Account(args) => args.execute(&client),

        Command::Location(command) => match command.command {
            LocationCommands::Create(args) => args.execute(&client),
            LocationCommands::Update(args) => args.execute(&client),
            LocationCommands::List(args) => args.execute(&client),
            LocationCommands::Get(args) => args.execute(&client),
            LocationCommands::Delete(args) => args.execute(&client),
        },
        Command::Exchange(command) => match command.command {
            ExchangeCommands::Create(args) => args.execute(&client),
            ExchangeCommands::Update(args) => args.execute(&client),
            ExchangeCommands::List(args) => args.execute(&client),
            ExchangeCommands::Get(args) => args.execute(&client),
            ExchangeCommands::Delete(args) => args.execute(&client),
        },
        Command::Device(command) => match command.command {
            DeviceCommands::Create(args) => args.execute(&client),
            DeviceCommands::Update(args) => args.execute(&client),
            DeviceCommands::List(args) => args.execute(&client),
            DeviceCommands::Get(args) => args.execute(&client),
            DeviceCommands::Delete(args) => args.execute(&client),
            DeviceCommands::Allowlist(command) => match command.command {
                DeviceAllowlistCommands::List(args) => args.execute(&client),
                DeviceAllowlistCommands::Add(args) => args.execute(&client),
                DeviceAllowlistCommands::Remove(args) => args.execute(&client),
            },
        },
        Command::Tunnel(command) => match command.command {
            TunnelCommands::Create(args) => args.execute(&client),
            TunnelCommands::Update(args) => args.execute(&client),
            TunnelCommands::List(args) => args.execute(&client),
            TunnelCommands::Get(args) => args.execute(&client),
            TunnelCommands::Delete(args) => args.execute(&client),
        },
        Command::User(command) => match command.command {
            UserCommands::Create(args) => args.execute(&client),
            UserCommands::Update(args) => args.execute(&client),
            UserCommands::List(args) => args.execute(&client),
            UserCommands::Get(args) => args.execute(&client),
            UserCommands::Delete(args) => args.execute(&client),
            UserCommands::Allowlist(command) => match command.command {
                UserAllowlistCommands::List(args) => args.execute(&client),
                UserAllowlistCommands::Add(args) => args.execute(&client),
                UserAllowlistCommands::Remove(args) => args.execute(&client),
            },
            UserCommands::RequestBan(args) => args.execute(&client),
        },
        Command::Export(args) => args.execute(&client),
        Command::Keygen(args) => args.execute(&client),
        Command::Log(args) => args.execute(&client),
    };

    match res {
        Ok(_) => {}
        Err(e) => {
            eprintln!("{}: {}", "Error".red(), e);
            std::process::exit(1);
        }
    };

    Ok(())
}
