use clap::Parser;
use colored::Colorize;

use config::ConfigCommands;
use device::DeviceCommands;
use double_zero_sdk::DZClient;

mod command;
mod config;
mod device;
mod exchange;
mod globalconfig;
mod location;
mod tunnel;
mod user;

use command::Command;
use exchange::ExchangeCommands;
use globalconfig::GlobalConfigCommands;
use location::LocationCommands;
use tunnel::TunnelCommands;
use user::UserCommands;


include!(concat!(env!("OUT_DIR"), "/version.rs"));

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "DoubleZero")]
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
        Command::Address(args) => args.execute(&client).await,
        Command::Balance(args) => args.execute(&client).await,

        Command::Init(args) => args.execute(&client).await,
        Command::Config(command) => match command.command {
            ConfigCommands::Get(args) => args.execute(&client).await,
            ConfigCommands::Set(args) => args.execute(&client).await,
        },
        Command::GlobalConfig(command) => match command.command {
            GlobalConfigCommands::Set(args) => args.execute(&client).await,
            GlobalConfigCommands::Get(args) => args.execute(&client).await,
            GlobalConfigCommands::Allowlist(command) => match command.command {
                crate::globalconfig::AllowlistCommands::Get(args) => args.execute(&client).await,
                crate::globalconfig::AllowlistCommands::Add(args) => args.execute(&client).await,
                crate::globalconfig::AllowlistCommands::Remove(args) => args.execute(&client).await,
            },
        },

        Command::Account(args) => args.execute(&client).await,

        Command::Location(command) => match command.command {
            LocationCommands::Create(args) => args.execute(&client).await,
            LocationCommands::Update(args) => args.execute(&client).await,
            LocationCommands::List(args) => args.execute(&client).await,
            LocationCommands::Get(args) => args.execute(&client).await,
            LocationCommands::Delete(args) => args.execute(&client).await,
        },
        Command::Exchange(command) => match command.command {
            ExchangeCommands::Create(args) => args.execute(&client).await,
            ExchangeCommands::Update(args) => args.execute(&client).await,
            ExchangeCommands::List(args) => args.execute(&client).await,
            ExchangeCommands::Get(args) => args.execute(&client).await,
            ExchangeCommands::Delete(args) => args.execute(&client).await,
        },
        Command::Device(command) => match command.command {
            DeviceCommands::Create(args) => args.execute(&client).await,
            DeviceCommands::Update(args) => args.execute(&client).await,
            DeviceCommands::List(args) => args.execute(&client).await,
            DeviceCommands::Get(args) => args.execute(&client).await,
            DeviceCommands::Delete(args) => args.execute(&client).await,
            DeviceCommands::Allowlist(command) => match command.command {
                crate::device::AllowlistCommands::Get(args) => args.execute(&client).await,
                crate::device::AllowlistCommands::Add(args) => args.execute(&client).await,
                crate::device::AllowlistCommands::Remove(args) => args.execute(&client).await, 
            }
        },
        Command::Tunnel(command) => match command.command {
            TunnelCommands::Create(args) => args.execute(&client).await,
            TunnelCommands::Update(args) => args.execute(&client).await,
            TunnelCommands::List(args) => args.execute(&client).await,
            TunnelCommands::Get(args) => args.execute(&client).await,
            TunnelCommands::Delete(args) => args.execute(&client).await,
        },
        Command::User(command) => match command.command {
            UserCommands::Create(args) => args.execute(&client).await,
            UserCommands::Update(args) => args.execute(&client).await,
            UserCommands::List(args) => args.execute(&client).await,
            UserCommands::Get(args) => args.execute(&client).await,
            UserCommands::Delete(args) => args.execute(&client).await,
            UserCommands::Allowlist(command) => match command.command {
                crate::user::AllowlistCommands::Get(args) => args.execute(&client).await,
                crate::user::AllowlistCommands::Add(args) => args.execute(&client).await,
                crate::user::AllowlistCommands::Remove(args) => args.execute(&client).await,
            },
            UserCommands::RequestBan(args) => args.execute(&client).await,
        },
        Command::Export(args) => args.execute(&client).await,
        Command::Keygen(args) => args.execute(&client).await,
        Command::Log(args) => args.execute(&client).await,
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
