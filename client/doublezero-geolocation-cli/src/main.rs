use clap::Parser;
use std::path::PathBuf;

mod cli;
use cli::{command::Command, config::ConfigCommands, probe::ProbeCommands, user::UserCommands};
use doublezero_cli::geoclicommand::GeoCliCommandImpl;
use doublezero_config::Environment;
use doublezero_sdk::{geolocation::client::GeoClient, DZClient};
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
    unsafe {
        libc::signal(libc::SIGPIPE, libc::SIG_DFL);
    }

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

    // Write commands require a valid keypair; check early for a clear error.
    let needs_keypair = matches!(
        &app.command,
        Command::Probe(cmd) if !matches!(cmd.command, ProbeCommands::Get(_) | ProbeCommands::List(_))
    ) || matches!(
        &app.command,
        Command::User(cmd) if !matches!(cmd.command, UserCommands::Get(_) | UserCommands::List(_))
    ) || matches!(&app.command, Command::InitConfig(_));

    if needs_keypair {
        let keypair_path = match &app.keypair {
            Some(p) => p.clone(),
            None => {
                let (_, cfg) = doublezero_sdk::read_doublezero_config()?;
                cfg.keypair_path
            }
        };
        if !keypair_path.exists() {
            eyre::bail!("keypair file not found: {}", keypair_path.display());
        }
    }

    let svc_client = DZClient::new(
        url.clone(),
        None,
        Some(svc_program_id.to_string()),
        app.keypair.clone(),
    )?;
    let geoclient = GeoClient::new(url, geo_program_id, app.keypair)?;
    let (globalstate_pk, _) = get_globalstate_pda(&svc_program_id);
    let client = GeoCliCommandImpl::new(&geoclient, &svc_client, globalstate_pk);

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
        Command::User(cmd) => match cmd.command {
            UserCommands::Create(args) => args.execute(&client, &mut handle),
            UserCommands::Delete(args) => args.execute(&client, &mut handle),
            UserCommands::Get(args) => args.execute(&client, &mut handle),
            UserCommands::List(args) => args.execute(&client, &mut handle),
            UserCommands::AddTarget(args) => args.execute(&client, &mut handle),
            UserCommands::RemoveTarget(args) => args.execute(&client, &mut handle),
            UserCommands::SetResultDestination(args) => args.execute(&client, &mut handle),
            UserCommands::UpdatePayment(args) => args.execute(&client, &mut handle),
        },
        Command::InitConfig(args) => args.execute(&client, &mut handle),
        // Config commands are handled by the early return above.
        Command::Config(_) => eyre::bail!("unexpected: config commands should be handled earlier"),
    }
}
