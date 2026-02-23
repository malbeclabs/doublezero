use clap::Parser;
use std::path::PathBuf;

mod cli;
use cli::{command::Command, config::ConfigCommands, probe::ProbeCommands};
use doublezero_cli::geoclicommand::GeoCliCommandImpl;
use doublezero_config::Environment;
use doublezero_sdk::geolocation::client::GeoClient;
use doublezero_serviceability::pda::get_globalstate_pda;

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "doublezero-geolocation")]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero Geolocation management tool")]
struct App {
    #[command(subcommand)]
    command: Command,
    /// DZ env (testnet, devnet, or mainnet-beta)
    #[arg(short, long, value_name = "ENV", global = true)]
    env: Option<String>,
    /// DZ ledger RPC URL
    #[arg(long, value_name = "RPC_URL", global = true)]
    url: Option<String>,
    /// Geolocation program ID
    #[arg(long, value_name = "PROGRAM_ID", global = true)]
    geo_program_id: Option<String>,
    /// Serviceability program ID (for GlobalState PDA derivation)
    #[arg(long, value_name = "PROGRAM_ID", global = true)]
    serviceability_program_id: Option<String>,
    /// Path to the keypair file
    #[arg(long, value_name = "KEYPAIR", global = true)]
    keypair: Option<PathBuf>,
}

fn main() -> eyre::Result<()> {
    let app = App::parse();

    let stdout = std::io::stdout();
    let mut handle = stdout.lock();

    // Config commands don't need an RPC connection
    if let Command::Config(cmd) = app.command {
        return match cmd.command {
            ConfigCommands::Get(args) => args.execute(&mut handle),
            ConfigCommands::Set(args) => args.execute(&mut handle),
        };
    }

    let (url, geo_program_id, svc_program_id) = if let Some(env) = app.env {
        let config = env.parse::<Environment>()?.config()?;
        (
            Some(config.ledger_public_rpc_url),
            Some(config.geolocation_program_id.to_string()),
            config.serviceability_program_id,
        )
    } else {
        let svc_pid = match &app.serviceability_program_id {
            Some(id) => id.parse::<solana_sdk::pubkey::Pubkey>()?,
            None => {
                let (_, cfg) = doublezero_sdk::read_doublezero_config()?;
                match cfg.program_id {
                    Some(id) => id.parse()?,
                    None => doublezero_sdk::default_program_id(),
                }
            }
        };
        (app.url, app.geo_program_id, svc_pid)
    };

    let geoclient = GeoClient::new(url, geo_program_id, app.keypair)?;
    let (globalstate_pk, _) = get_globalstate_pda(&svc_program_id);
    let client = GeoCliCommandImpl::new(&geoclient, globalstate_pk);

    match app.command {
        Command::Probe(cmd) => match cmd.command {
            ProbeCommands::Create(args) => args.execute(&client, &mut handle),
            ProbeCommands::Update(args) => args.execute(&client, &mut handle),
            ProbeCommands::Delete(args) => args.execute(&client, &mut handle),
            ProbeCommands::Get(args) => args.execute(&client, &mut handle),
            ProbeCommands::List(args) => args.execute(&client, &mut handle),
            ProbeCommands::AddParent(args) => args.execute(&client, &mut handle),
            ProbeCommands::RemoveParent(args) => args.execute(&client, &mut handle),
        },
        Command::InitConfig(args) => args.execute(&client, &mut handle),
        Command::Config(_) => unreachable!(),
    }
}
