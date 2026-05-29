use clap::{CommandFactory, Parser};
use clap_complete::generate;
use std::path::PathBuf;
mod cli;
mod command;
mod dzd_latency;
mod routes;
use doublezero_config::Environment;
mod requirements;
mod servicecontroller;
use crate::cli::{command::Command, multicast::MulticastCommands};
use doublezero_cli_core::LogLevel;
use doublezero_geolocation_cli::GeoCliCommandImpl;
use doublezero_sdk::{geolocation::client::GeoClient, DZClient, ProgramVersion};
use doublezero_serviceability::pda::get_globalstate_pda;
use doublezero_serviceability_cli::{
    checkversion::check_version, cli::ServiceabilityCommand, doublezerocommand::CliCommandImpl,
    version::VersionCliCommand,
};
use servicecontroller::ServiceControllerImpl;

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "DoubleZero")]
#[command(disable_version_flag = true)]
#[command(about = "DoubleZero client tool", long_about = None)]
struct App {
    #[command(subcommand)]
    command: Option<Command>,
    /// DZ env (testnet, devnet, or mainnet-beta).
    ///
    /// Mutually exclusive with the per-field URL and program-ID overrides
    /// (`--url`, `--ws`, `--solana-url`, `--program-id`, `--geo-program-id`).
    /// Pass `--env` to use a network's defaults wholesale, or pass the
    /// individual overrides; combining the two yields an error from clap.
    #[arg(
        short,
        long,
        value_name = "ENV",
        global = true,
        conflicts_with_all = ["url", "ws", "solana_url", "program_id", "geo_program_id"],
    )]
    env: Option<String>,
    /// DZ ledger RPC URL
    #[arg(long, value_name = "RPC_URL", global = true)]
    url: Option<String>,
    /// DZ ledger WebSocket URL
    #[arg(long, value_name = "WEBSOCKET_URL", global = true)]
    ws: Option<String>,
    /// Solana L1 RPC URL override (does not affect the DZ ledger)
    #[arg(long, value_name = "SOLANA_RPC_URL", global = true)]
    solana_url: Option<String>,
    /// DZ program ID (testnet or devnet)
    #[arg(long, value_name = "PROGRAM_ID", global = true)]
    program_id: Option<String>,
    /// Geolocation program ID
    #[arg(long, value_name = "GEO_PROGRAM_ID", global = true)]
    geo_program_id: Option<String>,
    /// Path to the keypair file
    #[arg(long, value_name = "KEYPAIR", global = true)]
    keypair: Option<PathBuf>,
    /// Path to the doublezerod Unix socket
    #[arg(
        long = "sock-file",
        alias = "socket",
        alias = "socket-path",
        value_name = "SOCK_FILE",
        global = true
    )]
    sock_file: Option<PathBuf>,
    /// Suppress version warning output
    #[arg(long, global = true)]
    no_version_warning: bool,
    /// Diagnostic logging level. One of: `off`, `error`, `warn` (default), `info`, `debug`, `trace`.
    #[arg(
        long = "log-level",
        value_name = "LEVEL",
        value_enum,
        default_value_t = LogLevel::default(),
        global = true,
    )]
    log_level: LogLevel,
    /// Print version information
    #[arg(short = 'V', long = "version", action = clap::ArgAction::SetTrue)]
    version: bool,
}

/// Resolve the active [`Environment`] from the `--env` flag and any persisted
/// config, falling back to the build-configured default
/// ([`doublezero_sdk::default_environment`]) when neither selects one.
///
/// The fallback MUST use the compiled default — not [`Environment::default`],
/// which is always `Devnet` — so testnet / mainnet-beta builds honor their
/// baked-in environment when no `config.yml` is present. `persisted` is `Some`
/// only when a config file actually exists on disk.
fn resolve_environment(
    env_flag: Option<&str>,
    persisted: Option<&doublezero_sdk::ClientConfig>,
) -> eyre::Result<Environment> {
    match env_flag {
        Some(s) => s.parse::<Environment>(),
        None => Ok(persisted
            .and_then(|c| c.program_id.as_deref())
            .and_then(|pid| Environment::from_program_id(pid).ok())
            .unwrap_or_else(doublezero_sdk::default_environment)),
    }
}

#[tokio::main]
async fn main() -> eyre::Result<()> {
    unsafe {
        libc::signal(libc::SIGPIPE, libc::SIG_DFL);
    }

    let app = App::parse();

    doublezero_cli_core::init_logging(app.log_level);

    if let Some(sock_file) = &app.sock_file {
        ServiceControllerImpl::set_global_socket_path(sock_file.to_string_lossy());
    }

    if let Some(keypair) = &app.keypair {
        tracing::info!(keypair = %keypair.display(), "using keypair");
    }

    // Resolve global configuration into a CliContext per RFC-20 (§CliContext).
    // The binary populates it once at startup; future verbs read from it.
    //
    // Precedence (highest wins): CLI flag > persisted `config.yml` > env-derived
    // default. File reads happen here in the binary; module crates only read
    // resolved values from `CliContext` (RFC-20 §67).
    let (persisted_path, persisted) =
        doublezero_sdk::read_doublezero_config().unwrap_or_else(|_| {
            (
                std::path::PathBuf::new(),
                doublezero_sdk::ClientConfig::default(),
            )
        });
    let persisted_exists = persisted_path.is_file();

    let env_explicit = app.env.is_some();
    let env = resolve_environment(app.env.as_deref(), persisted_exists.then_some(&persisted))
        .unwrap_or_else(|e| {
            doublezero_cli_core::error::render_eyre(&e);
            std::process::exit(1);
        });

    let mut ctx_builder = doublezero_cli_core::CliContextBuilder::new().with_env(env);

    // Layer the persisted config when the file exists. When the user is
    // selecting an environment wholesale via `--env`, skip persisted URL and
    // program-ID values so we never mix environments; the keypair path is
    // orthogonal to env and stays.
    if persisted_exists {
        if !env_explicit {
            ctx_builder = ctx_builder.with_ledger_rpc_url(persisted.json_rpc_url.clone());
            if let Some(ws) = persisted.websocket_url.clone() {
                ctx_builder = ctx_builder.with_ledger_ws_rpc_url(ws);
            }
            if let Some(pid) = persisted
                .program_id
                .as_deref()
                .and_then(|s| s.parse::<solana_sdk::pubkey::Pubkey>().ok())
            {
                ctx_builder = ctx_builder.with_serviceability_program_id(pid);
            }
            if let Some(pid) = persisted
                .geo_program_id
                .as_deref()
                .and_then(|s| s.parse::<solana_sdk::pubkey::Pubkey>().ok())
            {
                ctx_builder = ctx_builder.with_geolocation_program_id(pid);
            }
        }
        ctx_builder = ctx_builder.with_keypair_path(persisted.keypair_path.clone());
    }

    // CLI-flag overrides win. `--env` is mutually exclusive with the per-field
    // URL and program-ID flags at the clap layer, so at most one branch of each
    // pair fires per invocation.
    if let Some(u) = app.url.clone() {
        ctx_builder = ctx_builder.with_ledger_rpc_url(u);
    }
    if let Some(w) = app.ws.clone() {
        ctx_builder = ctx_builder.with_ledger_ws_rpc_url(w);
    }
    if let Some(s) = app.solana_url.clone() {
        ctx_builder = ctx_builder.with_solana_l1_rpc_url(s);
    }
    if let Some(pid) = app
        .program_id
        .as_deref()
        .and_then(|s| s.parse::<solana_sdk::pubkey::Pubkey>().ok())
    {
        ctx_builder = ctx_builder.with_serviceability_program_id(pid);
    }
    if let Some(pid) = app
        .geo_program_id
        .as_deref()
        .and_then(|s| s.parse::<solana_sdk::pubkey::Pubkey>().ok())
    {
        ctx_builder = ctx_builder.with_geolocation_program_id(pid);
    }
    if let Some(k) = app.keypair.clone() {
        ctx_builder = ctx_builder.with_keypair_path(k);
    }
    if let Some(s) = app.sock_file.clone() {
        ctx_builder = ctx_builder.with_daemon_socket_path(s);
    }
    let ctx = ctx_builder.build().unwrap_or_else(|e| {
        doublezero_cli_core::error::render_eyre(&e);
        std::process::exit(1);
    });

    // Build the SDK client directly from the resolved `CliContext`. The context
    // already carries the fully resolved URL/WS/program-ID, so `from_context`
    // consumes them verbatim (no config-file re-read, no moniker conversion).
    // The keypair argument reflects only the `--keypair` CLI flag so that the
    // SDK's `load_keypair` precedence chain (CLI flag > `DOUBLEZERO_KEYPAIR`
    // env var > stdin > context keypair path > default) is preserved. Passing
    // the layered ctx value as the CLI source would mask the env var, which the
    // e2e contributor-auth suite relies on for negative-authz checks.
    let dzclient = DZClient::from_context(&ctx, app.keypair.clone())?;
    let client = CliCommandImpl::new(&dzclient);

    let stdout = std::io::stdout();
    let mut handle = stdout.lock();

    if app.version {
        let local_version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"));
        return VersionCliCommand.execute(&client, local_version, &mut handle);
    }

    let command = match app.command {
        Some(cmd) => cmd,
        None => {
            App::command().print_help()?;
            println!();
            return Ok(());
        }
    };

    // Skip version check for verbs that should always work even if the program is unavailable.
    let skip_version_check = matches!(
        &command,
        Command::Status(_)
            | Command::Enable(_)
            | Command::Disable(_)
            | Command::Completion(_)
            | Command::Serviceability(
                ServiceabilityCommand::Address(_)
                    | ServiceabilityCommand::Balance(_)
                    | ServiceabilityCommand::Export(_),
            )
    );
    if !app.no_version_warning && !skip_version_check {
        let stderr = std::io::stderr();
        let mut err_handle = stderr.lock();
        check_version(&client, &mut err_handle, ProgramVersion::current())?;
    }

    let res = match command {
        // Daemon-control verbs (binary-local)
        Command::Connect(args) => args.execute(&client).await,
        Command::Enable(args) => args.execute(&client).await,
        Command::Disable(args) => args.execute(&client).await,
        Command::Status(args) => args.execute(&client).await,
        Command::Disconnect(args) => args.execute(&client).await,
        Command::Latency(args) => args.execute(&client).await,
        Command::Routes(args) => args.execute(&client).await,

        // Raw-DZClient diagnostic verbs
        Command::Account(args) => args.execute(&dzclient, &mut handle),
        Command::Accounts(args) => args.execute(&dzclient, &mut handle),
        Command::Log(args) => args.execute(&dzclient, &mut handle),

        // Geolocation module crate (doublezero-geolocation-cli per RFC-20)
        Command::Geolocation(args) => {
            let geo_client = GeoClient::from_context(&ctx, app.keypair.clone())?;
            let svc_program_id = *dzclient.get_program_id();
            let (globalstate_pk, _) = get_globalstate_pda(&svc_program_id);
            let geo_cli = GeoCliCommandImpl::new(&geo_client, &dzclient, globalstate_pk);
            args.command.execute(&ctx, &geo_cli, &mut handle).await
        }

        // Multicast: the `Group` subtree is module-crate business and dispatched by
        // `MulticastGroupCommands::execute`; the daemon-coupled async verbs
        // (Subscribe/Unsubscribe/Publish/Unpublish) stay binary-local because
        // they depend on `ServiceControllerImpl` and `resolve_client_ip`.
        Command::Multicast(args) => match args.command {
            MulticastCommands::Group(args) => {
                args.command.execute(&ctx, &client, &mut handle).await
            }
            MulticastCommands::Subscribe(args) => args.execute(&client).await,
            MulticastCommands::Unsubscribe(args) => args.execute(&client).await,
            MulticastCommands::Publish(args) => args.execute(&client).await,
            MulticastCommands::Unpublish(args) => args.execute(&client).await,
        },

        // Clap shell-completion generator (binary-local)
        Command::Completion(args) => {
            let mut cmd = App::command();
            generate(args.shell, &mut cmd, "doublezero", &mut std::io::stdout());
            Ok(())
        }

        // Flattened serviceability module: single dispatch arm hoists 17 variants.
        Command::Serviceability(cmd) => cmd.execute(&ctx, &client, &mut handle).await,
    };

    match res {
        Ok(_) => {}
        Err(e) => {
            doublezero_cli_core::error::render_eyre(&e);
            std::process::exit(1);
        }
    };

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::App;
    use clap::{error::ErrorKind, Parser};

    fn parse_err(args: &[&str]) -> clap::Error {
        App::try_parse_from(args).expect_err("expected clap to reject these arguments")
    }

    #[test]
    fn env_conflicts_with_url() {
        let err = parse_err(&[
            "doublezero",
            "--env",
            "devnet",
            "--url",
            "https://x.invalid/",
        ]);
        assert_eq!(err.kind(), ErrorKind::ArgumentConflict);
    }

    #[test]
    fn env_conflicts_with_ws() {
        let err = parse_err(&["doublezero", "--env", "devnet", "--ws", "wss://x.invalid/"]);
        assert_eq!(err.kind(), ErrorKind::ArgumentConflict);
    }

    #[test]
    fn env_conflicts_with_solana_url() {
        let err = parse_err(&[
            "doublezero",
            "--env",
            "devnet",
            "--solana-url",
            "https://x.invalid/",
        ]);
        assert_eq!(err.kind(), ErrorKind::ArgumentConflict);
    }

    #[test]
    fn env_conflicts_with_program_id() {
        let err = parse_err(&[
            "doublezero",
            "--env",
            "devnet",
            "--program-id",
            "11111111111111111111111111111111",
        ]);
        assert_eq!(err.kind(), ErrorKind::ArgumentConflict);
    }

    #[test]
    fn env_alone_parses() {
        App::try_parse_from(["doublezero", "--env", "devnet"]).expect("--env alone should parse");
    }

    #[test]
    fn url_alone_parses() {
        App::try_parse_from(["doublezero", "--url", "https://x.invalid/"])
            .expect("--url alone should parse");
    }

    use super::resolve_environment;
    use doublezero_config::Environment;

    // Regression: with no `--env` and no persisted config, the active
    // environment must be the build-configured default, not `Environment::default()`
    // (which is always Devnet). A testnet build must resolve to Testnet here.
    #[test]
    fn no_env_flag_no_config_uses_build_default() {
        assert_eq!(
            resolve_environment(None, None).unwrap(),
            doublezero_sdk::default_environment(),
        );
    }

    #[test]
    fn env_flag_overrides_default() {
        assert_eq!(
            resolve_environment(Some("mainnet-beta"), None).unwrap(),
            Environment::MainnetBeta,
        );
    }

    #[test]
    fn persisted_program_id_selects_its_environment() {
        let devnet_pid = Environment::Devnet
            .config()
            .unwrap()
            .serviceability_program_id
            .to_string();
        let cfg = doublezero_sdk::ClientConfig {
            program_id: Some(devnet_pid),
            ..Default::default()
        };
        assert_eq!(
            resolve_environment(None, Some(&cfg)).unwrap(),
            Environment::Devnet,
        );
    }

    #[test]
    fn persisted_config_without_program_id_uses_build_default() {
        let cfg = doublezero_sdk::ClientConfig {
            program_id: None,
            ..Default::default()
        };
        assert_eq!(
            resolve_environment(None, Some(&cfg)).unwrap(),
            doublezero_sdk::default_environment(),
        );
    }
}
