use double_zero_sdk::cli::config::ConfigCommands;

use clap::Parser;
use colored::Colorize;

use command::Command;
use double_zero_sdk::DZClient;

mod command;
mod helpers;
mod requirements;

include!(concat!(env!("OUT_DIR"), "/version.rs"));

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "DoubleZero")]
#[command(version = APP_VERSION)]
#[command(long_version = APP_LONG_VERSION)]
#[command(about = "Double Zero client tool", long_about = None)]
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
        Command::Connect(args) => args.execute(&client).await,
        Command::Status(args) => args.execute(&client).await,
        Command::Disconnect(args) => args.execute(&client).await,
        Command::Latency(args) => args.execute(&client).await,
        Command::Devices(args) => args.execute(&client).await,

        Command::Init(args) => args.execute(&client).await,
        Command::Config(command) => match command.command {
            ConfigCommands::Get(args) => args.execute(&client).await,
            ConfigCommands::Set(args) => args.execute(&client).await,
        },
        Command::Account(args) => args.execute(&client).await,


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
