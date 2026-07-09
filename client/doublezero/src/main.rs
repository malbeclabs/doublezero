use clap::{CommandFactory, Parser};
use clap_complete::generate;
use std::path::PathBuf;
mod cli;
mod command;
use doublezero_config::Environment;
mod servicecontroller;
use crate::cli::{command::Command, multicast::MulticastCommands, sentinel::SentinelCommands};
use doublezero_cli_core::LogLevel;
use doublezero_daemon_cli::{DaemonClientImpl, DaemonCommand};
use doublezero_geolocation_cli::GeoCliCommandImpl;
use doublezero_sdk::{
    convert_geo_program_moniker, convert_program_moniker, geolocation::client::GeoClient, DZClient,
    ProgramVersion,
};
use doublezero_serviceability::pda::get_globalstate_pda;
use doublezero_serviceability_cli::{
    checkversion::check_version,
    cli::ServiceabilityCommand,
    doublezerocommand::{CliCommand, CliCommandImpl},
    requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON},
};
use servicecontroller::ServiceControllerImpl;

/// Adapter bridging the binary's `CliCommand` to the daemon-cli crate's
/// `LedgerClient` trait. Holds the client so ledger-backed reads/writes (e.g.
/// the user teardown used by `disconnect`, the device list used by `latency`)
/// route through the SDK.
struct LedgerAdapter<'a, C: CliCommand> {
    env: Environment,
    client: &'a C,
}

impl<C: CliCommand + Sync> doublezero_daemon_cli::LedgerClient for LedgerAdapter<'_, C> {
    fn get_environment(&self) -> Environment {
        self.env
    }

    fn get_payer(&self) -> solana_sdk::pubkey::Pubkey {
        self.client.get_payer()
    }

    fn check_requirements(&self) -> eyre::Result<()> {
        check_requirements(self.client, None, CHECK_ID_JSON | CHECK_BALANCE)
    }

    fn get_globalstate(&self) -> eyre::Result<doublezero_sdk::GlobalState> {
        let (_, gstate) = self
            .client
            .get_globalstate(doublezero_sdk::GetGlobalStateCommand)?;
        Ok(gstate)
    }

    fn list_user(
        &self,
    ) -> eyre::Result<std::collections::HashMap<solana_sdk::pubkey::Pubkey, doublezero_sdk::User>>
    {
        self.client
            .list_user(doublezero_sdk::commands::user::list::ListUserCommand)
    }

    fn delete_user(&self, pubkey: solana_sdk::pubkey::Pubkey) -> eyre::Result<()> {
        self.client
            .delete_user(doublezero_sdk::commands::user::delete::DeleteUserCommand { pubkey })?;
        Ok(())
    }

    fn get_user(&self, pubkey: solana_sdk::pubkey::Pubkey) -> eyre::Result<doublezero_sdk::User> {
        let (_, user) = self
            .client
            .get_user(doublezero_sdk::commands::user::get::GetUserCommand { pubkey })?;
        Ok(user)
    }

    fn list_device(
        &self,
    ) -> eyre::Result<std::collections::HashMap<solana_sdk::pubkey::Pubkey, doublezero_sdk::Device>>
    {
        self.client
            .list_device(doublezero_sdk::commands::device::list::ListDeviceCommand)
    }

    fn get_epoch(&self) -> eyre::Result<u64> {
        self.client.get_epoch()
    }

    fn get_accesspass(
        &self,
        client_ip: std::net::Ipv4Addr,
        user_payer: solana_sdk::pubkey::Pubkey,
    ) -> eyre::Result<Option<doublezero_serviceability::state::accesspass::AccessPass>> {
        Ok(self
            .client
            .get_accesspass(
                doublezero_sdk::commands::accesspass::get::GetAccessPassCommand {
                    client_ip,
                    user_payer,
                },
            )?
            .map(|(_, accesspass)| accesspass))
    }

    fn get_device(&self, pubkey_or_code: String) -> eyre::Result<doublezero_sdk::Device> {
        let (_, device) =
            self.client
                .get_device(doublezero_sdk::commands::device::get::GetDeviceCommand {
                    pubkey_or_code,
                })?;
        Ok(device)
    }

    fn get_tenant(
        &self,
        pubkey_or_code: String,
    ) -> eyre::Result<(solana_sdk::pubkey::Pubkey, doublezero_sdk::Tenant)> {
        self.client
            .get_tenant(doublezero_sdk::commands::tenant::get::GetTenantCommand { pubkey_or_code })
    }

    fn list_multicastgroup(
        &self,
    ) -> eyre::Result<
        std::collections::HashMap<solana_sdk::pubkey::Pubkey, doublezero_sdk::MulticastGroup>,
    > {
        self.client.list_multicastgroup(
            doublezero_sdk::commands::multicastgroup::list::ListMulticastGroupCommand,
        )
    }

    fn create_user(
        &self,
        cmd: doublezero_sdk::commands::user::create::CreateUserCommand,
    ) -> eyre::Result<solana_sdk::pubkey::Pubkey> {
        let (_, pubkey) = self.client.create_user(cmd)?;
        Ok(pubkey)
    }

    fn create_subscribe_user(
        &self,
        cmd: doublezero_sdk::commands::user::create_subscribe::CreateSubscribeUserCommand,
    ) -> eyre::Result<solana_sdk::pubkey::Pubkey> {
        let (_, pubkey) = self.client.create_subscribe_user(cmd)?;
        Ok(pubkey)
    }

    fn update_multicastgroup_roles(
        &self,
        cmd: doublezero_sdk::commands::multicastgroup::subscribe::UpdateMulticastGroupRolesCommand,
    ) -> eyre::Result<()> {
        self.client.update_multicastgroup_roles(cmd)?;
        Ok(())
    }
}

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
    /// `--env` resolves a whole network at once (ledger URL, WS URL, Solana L1
    /// URL, serviceability and geolocation program IDs). The per-field flags
    /// (`--url`, `--ws`, `--solana-url`, `--program-id`, `--geo-program-id`)
    /// override individual values on top of that base. Precedence per RFC-20
    /// (§override hierarchy): explicit CLI flag > env var > value from `--env`.
    #[arg(short, long, value_name = "ENV", global = true)]
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

/// Resolve a `--program-id` / `--geo-program-id` flag value into a `Pubkey`.
///
/// The raw value is first run through `convert` so environment monikers (in
/// either their full `devnet` or short `d` form) map to the matching program
/// ID; a literal pubkey passes through unchanged. A value that is neither a
/// known moniker nor a valid pubkey is a hard error rather than being silently
/// dropped in favor of the env default.
fn resolve_program_id(
    flag: &str,
    raw: &str,
    convert: impl Fn(String) -> String,
) -> eyre::Result<solana_sdk::pubkey::Pubkey> {
    let converted = convert(raw.to_string());
    converted.parse().map_err(|_| {
        eyre::eyre!(
            "invalid {flag} '{raw}': expected a pubkey or a known environment \
             moniker (mainnet-beta/m, testnet/t, devnet/d, local/l)"
        )
    })
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
        DaemonClientImpl::set_global_socket_path(sock_file.to_string_lossy());
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

    let local_version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION"));
    let mut ctx_builder = doublezero_cli_core::CliContextBuilder::new()
        .with_env(env)
        .with_client_version(local_version);

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

    // CLI-flag overrides win, layered on top of the `--env` base resolved into
    // the builder above: each per-field flag replaces only its own value, while
    // the rest keep following `--env` (RFC-20 §override hierarchy).
    if let Some(u) = app.url.clone() {
        ctx_builder = ctx_builder.with_ledger_rpc_url(u);
    }
    if let Some(w) = app.ws.clone() {
        ctx_builder = ctx_builder.with_ledger_ws_rpc_url(w);
    }
    if let Some(s) = app.solana_url.clone() {
        ctx_builder = ctx_builder.with_solana_l1_rpc_url(s);
    }
    if let Some(s) = app.program_id.as_deref() {
        let pid = resolve_program_id("--program-id", s, convert_program_moniker)?;
        ctx_builder = ctx_builder.with_serviceability_program_id(pid);
    }
    if let Some(s) = app.geo_program_id.as_deref() {
        let pid = resolve_program_id("--geo-program-id", s, convert_geo_program_moniker)?;
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
    let has_keypair_source = app.keypair.is_some()
        || std::env::var(doublezero_sdk::keypair::ENV_KEYPAIR).is_ok()
        || !std::io::IsTerminal::is_terminal(&std::io::stdin());
    let client = CliCommandImpl::new(&dzclient).with_keypair_source(has_keypair_source);

    let stdout = std::io::stdout();
    let mut handle = stdout.lock();

    if app.version {
        return doublezero_serviceability_cli::version::VersionCliCommand
            .execute(&ctx, &client, &mut handle)
            .await;
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
        Command::Daemon(
            DaemonCommand::Enable(_) | DaemonCommand::Disable(_) | DaemonCommand::Status(_)
        ) | Command::Completion(_)
            | Command::Serviceability(
                ServiceabilityCommand::Address(_)
                    | ServiceabilityCommand::Balance(_)
                    | ServiceabilityCommand::Export(_)
                    | ServiceabilityCommand::Version(_),
            )
    );
    if !app.no_version_warning && !skip_version_check {
        let stderr = std::io::stderr();
        let mut err_handle = stderr.lock();
        check_version(&client, &mut err_handle, ProgramVersion::current())?;
    }

    let res = match command {
        // Daemon-control verbs migrated to doublezero-daemon-cli (RFC-20)
        Command::Daemon(cmd) => {
            let daemon = DaemonClientImpl::new(
                ctx.daemon_socket_path
                    .as_ref()
                    .map(|p| p.to_string_lossy().into_owned()),
            );
            let ledger = LedgerAdapter {
                env: client.get_environment(),
                client: &client,
            };
            cmd.execute(&ctx, &daemon, &ledger, &mut handle).await
        }

        // Sentinel admin commands (binary-local): they take `DZClient` directly
        // and write their own output, so no `ctx`/writer threading is needed.
        Command::Sentinel(cmd) => match cmd.command {
            SentinelCommands::FindValidatorMulticastPublishers(args) => {
                args.execute(&dzclient).await
            }
            SentinelCommands::CreateValidatorMulticastPublishers(args) => {
                args.execute(&dzclient).await
            }
        },

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

        // Binary-level override: subscribe uses the real blocking websocket
        // loop (DZClient::subscribe) for live event streaming. The module
        // crate's SubscribeCliCommand.execute() falls back to a get_all()
        // snapshot for testability (mockall cannot mock FnMut callbacks).
        Command::Serviceability(ServiceabilityCommand::Subscribe(_)) => {
            use std::{
                io::Write,
                sync::{atomic::AtomicBool, Arc},
            };
            writeln!(handle, "Waiting for events...")?;
            let stop = Arc::new(AtomicBool::new(false));
            dzclient.subscribe(
                |_client, pubkey, account| {
                    let _ = writeln!(handle, "{pubkey} -> {account:?}");
                },
                stop,
            )?;
            Ok(())
        }

        // Flattened serviceability module: single dispatch arm hoists all variants.
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
    use clap::Parser;

    fn parse_ok(args: &[&str]) -> App {
        App::try_parse_from(args).expect("expected clap to accept these arguments")
    }

    // `--env` layers with the per-field override flags rather than conflicting
    // with them: each combination must parse, with both values retained so the
    // override can win over the env base during resolution.

    #[test]
    fn env_combines_with_url() {
        let app = parse_ok(&[
            "doublezero",
            "--env",
            "devnet",
            "--url",
            "https://x.invalid/",
        ]);
        assert_eq!(app.env.as_deref(), Some("devnet"));
        assert_eq!(app.url.as_deref(), Some("https://x.invalid/"));
    }

    #[test]
    fn env_combines_with_ws() {
        let app = parse_ok(&["doublezero", "--env", "devnet", "--ws", "wss://x.invalid/"]);
        assert_eq!(app.env.as_deref(), Some("devnet"));
        assert_eq!(app.ws.as_deref(), Some("wss://x.invalid/"));
    }

    #[test]
    fn env_combines_with_solana_url() {
        let app = parse_ok(&[
            "doublezero",
            "--env",
            "devnet",
            "--solana-url",
            "https://x.invalid/",
        ]);
        assert_eq!(app.env.as_deref(), Some("devnet"));
        assert_eq!(app.solana_url.as_deref(), Some("https://x.invalid/"));
    }

    #[test]
    fn env_combines_with_program_id() {
        let app = parse_ok(&[
            "doublezero",
            "--env",
            "devnet",
            "--program-id",
            "11111111111111111111111111111111",
        ]);
        assert_eq!(app.env.as_deref(), Some("devnet"));
        assert_eq!(
            app.program_id.as_deref(),
            Some("11111111111111111111111111111111")
        );
    }

    #[test]
    fn env_combines_with_geo_program_id() {
        let app = parse_ok(&[
            "doublezero",
            "--env",
            "devnet",
            "--geo-program-id",
            "11111111111111111111111111111111",
        ]);
        assert_eq!(app.env.as_deref(), Some("devnet"));
        assert_eq!(
            app.geo_program_id.as_deref(),
            Some("11111111111111111111111111111111")
        );
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

    #[test]
    fn sentinel_subcommands_parse() {
        App::try_parse_from([
            "doublezero",
            "sentinel",
            "find-validator-multicast-publishers",
        ])
        .expect("find parses");
        App::try_parse_from([
            "doublezero",
            "sentinel",
            "create-validator-multicast-publishers",
            "--multicast-group",
            "mg-test",
        ])
        .expect("create parses");
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

    use super::resolve_program_id;
    use doublezero_sdk::{convert_geo_program_moniker, convert_program_moniker};

    #[test]
    fn resolve_program_id_accepts_full_and_short_monikers() {
        for env in [
            Environment::MainnetBeta,
            Environment::Testnet,
            Environment::Devnet,
            Environment::Local,
        ] {
            let cfg = env.config().unwrap();
            for moniker in [
                env.to_string(),
                env.to_string().chars().next().unwrap().to_string(),
            ] {
                assert_eq!(
                    resolve_program_id("--program-id", &moniker, convert_program_moniker).unwrap(),
                    cfg.serviceability_program_id,
                    "serviceability moniker {moniker} should resolve to {env}",
                );
                assert_eq!(
                    resolve_program_id("--geo-program-id", &moniker, convert_geo_program_moniker)
                        .unwrap(),
                    cfg.geolocation_program_id,
                    "geo moniker {moniker} should resolve to {env}",
                );
            }
        }
    }

    #[test]
    fn resolve_program_id_passes_through_literal_pubkey() {
        let pk = solana_sdk::pubkey::Pubkey::new_unique();
        assert_eq!(
            resolve_program_id("--program-id", &pk.to_string(), convert_program_moniker).unwrap(),
            pk,
        );
    }

    #[test]
    fn resolve_program_id_rejects_invalid_value() {
        assert!(
            resolve_program_id("--program-id", "not-a-key", convert_program_moniker).is_err(),
            "an unparseable value must be a hard error, not a silent fallback",
        );
    }
}
