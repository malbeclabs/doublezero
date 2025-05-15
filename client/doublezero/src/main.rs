use clap::Parser;
use crate::cli::device::DeviceAllowlistCommands;
use crate::cli::globalconfig::FoundationAllowlistCommands;
use clap_complete::generate;
use crate::cli::user::UserAllowlistCommands;
use crate::cli::{
    command::Command, config::ConfigCommands, device::DeviceCommands, exchange::ExchangeCommands,
    globalconfig::GlobalConfigCommands, location::LocationCommands, tunnel::TunnelCommands,
    user::UserCommands,
};
use doublezero_cli::doublezerocommand::CliCommandImpl;
use doublezero_sdk::DZClient;
use clap::CommandFactory;

mod cli;
mod command;
mod requirements;
mod servicecontroller;
mod tests;

include!(concat!(env!("OUT_DIR"), "/version.rs"));

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "DoubleZero")]
#[command(version = APP_VERSION)]
#[command(long_version = APP_LONG_VERSION)]
#[command(about = "DoubleZero client tool", long_about = None)]
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
    let app = App::parse();

    if let Some(keypair) = &app.keypair {
        println!("using keypair: {}", keypair);
    }

    let dzclient = DZClient::new(app.url, app.ws, app.program_id, app.keypair)?;
    let client = CliCommandImpl::new(&dzclient);

    let stdout = std::io::stdout();
    let mut handle = stdout.lock();

    let res = match app.command {
        Command::Address(args) => args.execute(&client, &mut handle),
        Command::Balance(args) => args.execute(&client, &mut handle),
        Command::Connect(args) => args.execute(&client).await,
        Command::Status(args) => args.execute(&client).await,
        Command::Disconnect(args) => args.execute(&client).await,
        Command::Latency(args) => args.execute(&client).await,

        Command::Init(args) => args.execute(&client, &mut handle),
        Command::Config(command) => match command.command {
            ConfigCommands::Get(args) => args.execute(&client, &mut handle),
            ConfigCommands::Set(args) => args.execute(&client, &mut handle),
        },
        Command::GlobalConfig(command) => match command.command {
            GlobalConfigCommands::Set(args) => args.execute(&client, &mut handle),
            GlobalConfigCommands::Get(args) => args.execute(&client, &mut handle),
            GlobalConfigCommands::Allowlist(command) => match command.command {
                FoundationAllowlistCommands::List(args) => args.execute(&client, &mut handle),
                FoundationAllowlistCommands::Add(args) => args.execute(&client, &mut handle),
                FoundationAllowlistCommands::Remove(args) => args.execute(&client, &mut handle),
            },
        },
        Command::Account(args) => args.execute(&dzclient, &mut handle),
        Command::Location(command) => match command.command {
            LocationCommands::Create(args) => args.execute(&client, &mut handle),
            LocationCommands::Update(args) => args.execute(&client, &mut handle),
            LocationCommands::List(args) => args.execute(&client, &mut handle),
            LocationCommands::Get(args) => args.execute(&client, &mut handle),
            LocationCommands::Delete(args) => args.execute(&client, &mut handle),
        },
        Command::Exchange(command) => match command.command {
            ExchangeCommands::Create(args) => args.execute(&client, &mut handle),
            ExchangeCommands::Update(args) => args.execute(&client, &mut handle),
            ExchangeCommands::List(args) => args.execute(&client, &mut handle),
            ExchangeCommands::Get(args) => args.execute(&client, &mut handle),
            ExchangeCommands::Delete(args) => args.execute(&client, &mut handle),
        },
        Command::Device(command) => match command.command {
            DeviceCommands::Create(args) => args.execute(&client, &mut handle),
            DeviceCommands::Update(args) => args.execute(&client, &mut handle),
            DeviceCommands::List(args) => args.execute(&client, &mut handle),
            DeviceCommands::Get(args) => args.execute(&client, &mut handle),
            DeviceCommands::Delete(args) => args.execute(&client, &mut handle),
            DeviceCommands::Allowlist(command) => match command.command {
                DeviceAllowlistCommands::List(args) => args.execute(&client, &mut handle),
                DeviceAllowlistCommands::Add(args) => args.execute(&client, &mut handle),
                DeviceAllowlistCommands::Remove(args) => args.execute(&client, &mut handle),
            },
        },
        Command::Tunnel(command) => match command.command {
            TunnelCommands::Create(args) => args.execute(&client, &mut handle),
            TunnelCommands::Update(args) => args.execute(&client, &mut handle),
            TunnelCommands::List(args) => args.execute(&client, &mut handle),
            TunnelCommands::Get(args) => args.execute(&client, &mut handle),
            TunnelCommands::Delete(args) => args.execute(&client, &mut handle),
        },
        Command::User(command) => match command.command {
            UserCommands::Create(args) => args.execute(&client, &mut handle),
            UserCommands::Update(args) => args.execute(&client, &mut handle),
            UserCommands::List(args) => args.execute(&client, &mut handle),
            UserCommands::Get(args) => args.execute(&client, &mut handle),
            UserCommands::Delete(args) => args.execute(&client, &mut handle),
            UserCommands::Allowlist(command) => match command.command {
                UserAllowlistCommands::List(args) => args.execute(&client, &mut handle),
                UserAllowlistCommands::Add(args) => args.execute(&client, &mut handle),
                UserAllowlistCommands::Remove(args) => args.execute(&client, &mut handle),
            },
            UserCommands::RequestBan(args) => args.execute(&client, &mut handle),
        },
        Command::Multicast(args) => match args.command {
            cli::multicast::MulticastCommands::Group(args) => match args.command {
                cli::multicastgroup::MulticastGroupCommands::Create(args) => {
                    args.execute(&client, &mut handle)
                }
                cli::multicastgroup::MulticastGroupCommands::Update(args) => {
                    args.execute(&client, &mut handle)
                }
                cli::multicastgroup::MulticastGroupCommands::List(args) => {
                    args.execute(&client, &mut handle)
                }
                cli::multicastgroup::MulticastGroupCommands::Get(args) => {
                    args.execute(&client, &mut handle)
                }
                cli::multicastgroup::MulticastGroupCommands::Delete(args) => {
                    args.execute(&client, &mut handle)
                }
            },
        },

        Command::Export(args) => args.execute(&client, &mut handle),
        Command::Keygen(args) => args.execute(&client, &mut handle),
        Command::Log(args) => args.execute(&dzclient, &mut handle),
        Command::Completion(args) => {
            let mut cmd = App::command();
            generate(args.shell, &mut cmd, "doublezero", &mut std::io::stdout());
            Ok(())
        }
    };

    match res {
        Ok(_) => {}
        Err(e) => {
            eprintln!("Error: {}", e);
            std::process::exit(1);
        }
    };

    Ok(())
}
