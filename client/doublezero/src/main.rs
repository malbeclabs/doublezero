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
use crate::cli::{
    command::Command,
    config::ConfigCommands,
    device::{DeviceCommands, InterfaceCommands},
    exchange::ExchangeCommands,
    geolocation::{
        probe::ProbeCommands, user::UserCommands as GeoUserCommands, GeolocationCommands,
    },
    globalconfig::{
        AirdropCommands, AuthorityCommands, FeatureFlagsCommands, FoundationAllowlistCommands,
        GlobalConfigCommands, QaAllowlistCommands,
    },
    link::{LinkCommands, TopologyCommands},
    location::LocationCommands,
    user::UserCommands,
};
use doublezero_cli_core::LogLevel;
use doublezero_sdk::{geolocation::client::GeoClient, DZClient, ProgramVersion};
use doublezero_serviceability::pda::get_globalstate_pda;
use doublezero_serviceability_cli::{
    checkversion::check_version, doublezerocommand::CliCommandImpl,
    geoclicommand::GeoCliCommandImpl, version::VersionCliCommand,
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
    let env = match app.env.as_deref() {
        Some(s) => s.parse::<Environment>().unwrap_or_else(|e| {
            doublezero_cli_core::error::render_eyre(&e);
            std::process::exit(1);
        }),
        None if persisted_exists => persisted
            .program_id
            .as_deref()
            .and_then(|pid| Environment::from_program_id(pid).ok())
            .unwrap_or_default(),
        None => Environment::default(),
    };

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

    // Bridge to the legacy `DZClient::new(Option<String>, ...)` signature.
    // CliContext now carries the fully resolved values for URL/WS/program-ID,
    // so we forward them directly. The keypair argument is an exception: it
    // must reflect only the `--keypair` CLI flag so that `DZClient::new`'s
    // internal `load_keypair` precedence chain (CLI flag > `DOUBLEZERO_KEYPAIR`
    // env var > stdin > persisted config) is preserved. Passing the layered
    // ctx value here would mask the env var, which the e2e contributor-auth
    // suite relies on for negative-authz checks.
    let url = Some(ctx.ledger_rpc_url.clone());
    let ws = Some(ctx.ledger_ws_rpc_url.clone());
    let program_id = Some(ctx.serviceability_program_id.to_string());

    let dzclient = DZClient::new(url.clone(), ws, program_id, app.keypair.clone())?;
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

    // Skip version check for Status command to allow checking status of services when the program is running
    let skip_version_check = matches!(
        &command,
        Command::Status(_)
            | Command::Enable(_)
            | Command::Disable(_)
            | Command::Address(_)
            | Command::Balance(_)
            | Command::Export(_)
            | Command::Completion(_)
    );
    if !app.no_version_warning && !skip_version_check {
        let stderr = std::io::stderr();
        let mut err_handle = stderr.lock();
        check_version(&client, &mut err_handle, ProgramVersion::current())?;
    }

    let res = match command {
        Command::Address(args) => args.execute(&client, &mut handle),
        Command::Balance(args) => args.execute(&client, &mut handle),
        Command::Connect(args) => args.execute(&client).await,
        Command::Enable(args) => args.execute(&client).await,
        Command::Disable(args) => args.execute(&client).await,
        Command::Status(args) => args.execute(&client).await,
        Command::Disconnect(args) => args.execute(&client).await,
        Command::Latency(args) => args.execute(&client).await,
        Command::Routes(args) => args.execute(&client).await,

        Command::Init(args) => args.execute(&client, &mut handle),
        Command::Migrate(args) => args.execute(&client, &mut handle),
        Command::InitGeolocationConfig(args) => {
            let geo_client =
                GeoClient::new(url.clone(), app.geo_program_id.clone(), app.keypair.clone())?;
            let svc_program_id = *dzclient.get_program_id();
            let (globalstate_pk, _) = get_globalstate_pda(&svc_program_id);
            let geo_cli = GeoCliCommandImpl::new(&geo_client, &dzclient, globalstate_pk);
            args.execute(&geo_cli, &mut handle)
        }
        Command::Config(command) => match command.command {
            ConfigCommands::Get(args) => args.execute(&client, &mut handle),
            ConfigCommands::Set(args) => args.execute(&client, &mut handle),
        },
        Command::GlobalConfig(command) => match command.command {
            GlobalConfigCommands::Set(args) => args.execute(&client, &mut handle),
            GlobalConfigCommands::Get(args) => args.execute(&client, &mut handle),
            GlobalConfigCommands::Airdrop(command) => match command.command {
                AirdropCommands::Set(args) => args.execute(&client, &mut handle),
                AirdropCommands::Get(args) => args.execute(&client, &mut handle),
            },
            GlobalConfigCommands::Authority(command) => match command.command {
                AuthorityCommands::Set(args) => args.execute(&client, &mut handle),
                AuthorityCommands::Get(args) => args.execute(&client, &mut handle),
            },
            GlobalConfigCommands::Allowlist(command) => match command.command {
                FoundationAllowlistCommands::List(args) => args.execute(&client, &mut handle),
                FoundationAllowlistCommands::Add(args) => args.execute(&client, &mut handle),
                FoundationAllowlistCommands::Remove(args) => args.execute(&client, &mut handle),
            },
            GlobalConfigCommands::QaAllowlist(command) => match command.command {
                QaAllowlistCommands::List(args) => args.execute(&client, &mut handle),
                QaAllowlistCommands::Add(args) => args.execute(&client, &mut handle),
                QaAllowlistCommands::Remove(args) => args.execute(&client, &mut handle),
            },
            GlobalConfigCommands::SetVersion(args) => args.execute(&client, &mut handle),
            GlobalConfigCommands::FeatureFlags(command) => match command.command {
                FeatureFlagsCommands::Get(args) => args.execute(&client, &mut handle),
                FeatureFlagsCommands::Set(args) => args.execute(&client, &mut handle),
            },
        },
        Command::Account(args) => args.execute(&dzclient, &mut handle),
        Command::Accounts(args) => args.execute(&dzclient, &mut handle),
        Command::Location(command) => match command.command {
            LocationCommands::Create(args) => args.execute(&client, &mut handle),
            LocationCommands::Update(args) => args.execute(&client, &mut handle),
            LocationCommands::List(args) => args.execute(&client, &mut handle),
            LocationCommands::Get(args) => args.execute(&ctx, &client, &mut handle).await,
            LocationCommands::Delete(args) => args.execute(&client, &mut handle),
        },
        Command::Exchange(command) => match command.command {
            ExchangeCommands::Create(args) => args.execute(&client, &mut handle),
            ExchangeCommands::SetDevice(args) => args.execute(&client, &mut handle),
            ExchangeCommands::Update(args) => args.execute(&client, &mut handle),
            ExchangeCommands::List(args) => args.execute(&client, &mut handle),
            ExchangeCommands::Get(args) => args.execute(&client, &mut handle),
            ExchangeCommands::Delete(args) => args.execute(&client, &mut handle),
        },
        Command::Contributor(command) => match command.command {
            cli::contributor::ContributorCommands::Create(args) => {
                args.execute(&client, &mut handle)
            }
            cli::contributor::ContributorCommands::Update(args) => {
                args.execute(&client, &mut handle)
            }
            cli::contributor::ContributorCommands::List(args) => args.execute(&client, &mut handle),
            cli::contributor::ContributorCommands::Get(args) => args.execute(&client, &mut handle),
            cli::contributor::ContributorCommands::Delete(args) => {
                args.execute(&client, &mut handle)
            }
        },
        Command::Permission(command) => match command.command {
            cli::permission::PermissionCommands::Set(args) => args.execute(&client, &mut handle),
            cli::permission::PermissionCommands::Suspend(args) => {
                args.execute(&client, &mut handle)
            }
            cli::permission::PermissionCommands::Resume(args) => args.execute(&client, &mut handle),
            cli::permission::PermissionCommands::Delete(args) => args.execute(&client, &mut handle),
            cli::permission::PermissionCommands::Get(args) => args.execute(&client, &mut handle),
            cli::permission::PermissionCommands::List(args) => args.execute(&client, &mut handle),
        },
        Command::Tenant(command) => match command.command {
            cli::tenant::TenantCommands::Create(args) => args.execute(&client, &mut handle),
            cli::tenant::TenantCommands::Update(args) => args.execute(&client, &mut handle),
            cli::tenant::TenantCommands::List(args) => args.execute(&client, &mut handle),
            cli::tenant::TenantCommands::Get(args) => args.execute(&client, &mut handle),
            cli::tenant::TenantCommands::Delete(args) => args.execute(&client, &mut handle),
            cli::tenant::TenantCommands::Administrator(command) => match command.command {
                cli::tenant::AdministratorCommands::Add(args) => args.execute(&client, &mut handle),
                cli::tenant::AdministratorCommands::Remove(args) => {
                    args.execute(&client, &mut handle)
                }
            },
        },
        Command::Device(command) => match command.command {
            DeviceCommands::Create(args) => args.execute(&client, &mut handle),
            DeviceCommands::Update(args) => args.execute(&client, &mut handle),
            DeviceCommands::List(args) => args.execute(&client, &mut handle),
            DeviceCommands::Get(args) => args.execute(&client, &mut handle),
            DeviceCommands::Delete(args) => args.execute(&client, &mut handle),
            DeviceCommands::Interface(command) => match command.command {
                InterfaceCommands::Create(args) => args.execute(&client, &mut handle),
                InterfaceCommands::Update(args) => args.execute(&client, &mut handle),
                InterfaceCommands::List(args) => args.execute(&client, &mut handle),
                InterfaceCommands::Get(args) => args.execute(&client, &mut handle),
                InterfaceCommands::Delete(args) => args.execute(&client, &mut handle),
            },
            DeviceCommands::SetHealth(args) => args.execute(&client, &mut handle),
        },
        Command::Link(command) => match command.command {
            LinkCommands::Create(args) => match args.command {
                cli::link::CreateLinkCommands::Wan(args) => args.execute(&client, &mut handle),
                cli::link::CreateLinkCommands::Dzx(args) => args.execute(&client, &mut handle),
            },
            LinkCommands::Accept(args) => args.execute(&client, &mut handle),
            LinkCommands::Update(args) => args.execute(&client, &mut handle),
            LinkCommands::List(args) => args.execute(&client, &mut handle),
            LinkCommands::Get(args) => args.execute(&client, &mut handle),
            LinkCommands::Latency(args) => args.execute(&client, &mut handle),
            LinkCommands::Delete(args) => args.execute(&client, &mut handle),
            LinkCommands::SetHealth(args) => args.execute(&client, &mut handle),
            LinkCommands::Topology(args) => match args.command {
                TopologyCommands::Create(args) => args.execute(&client, &mut handle),
                TopologyCommands::Delete(args) => args.execute(&client, &mut handle),
                TopologyCommands::Clear(args) => args.execute(&client, &mut handle),
                TopologyCommands::AssignNodeSegments(args) => args.execute(&client, &mut handle),
                TopologyCommands::List(args) => args.execute(&client, &mut handle),
            },
        },
        Command::AccessPass(command) => match command.command {
            cli::accesspass::AccessPassCommands::Set(args) => args.execute(&client, &mut handle),
            cli::accesspass::AccessPassCommands::Close(args) => args.execute(&client, &mut handle),
            cli::accesspass::AccessPassCommands::List(args) => args.execute(&client, &mut handle),
            cli::accesspass::AccessPassCommands::Get(args) => args.execute(&client, &mut handle),
            cli::accesspass::AccessPassCommands::UserBalances(args) => {
                args.execute(&client, &mut handle)
            }
            cli::accesspass::AccessPassCommands::Fund(args) => {
                args.execute(&client, &mut handle, &mut std::io::stdin().lock())
            }
        },
        Command::User(command) => match command.command {
            UserCommands::Create(args) => args.execute(&client, &mut handle),
            UserCommands::CreateSubscribe(args) => args.execute(&client, &mut handle),
            UserCommands::Subscribe(args) => args.execute(&client, &mut handle),
            UserCommands::Update(args) => args.execute(&client, &mut handle),
            UserCommands::List(args) => args.execute(&client, &mut handle),
            UserCommands::Get(args) => args.execute(&client, &mut handle),
            UserCommands::Delete(args) => args.execute(&client, &mut handle),
            UserCommands::RequestBan(args) => args.execute(&client, &mut handle),
        },
        Command::Multicast(args) => match args.command {
            cli::multicast::MulticastCommands::Group(args) => match args.command {
                cli::multicastgroup::MulticastGroupCommands::Allowlist(args) => {
                    match args.command {
                        cli::multicastgroup::MulticastGroupAllowlistCommands::Publisher(args) => {
                            match args.command {
                                cli::multicastgroup::MulticastGroupPubAllowlistCommands::List(
                                    args,
                                ) => args.execute(&client, &mut handle),
                                cli::multicastgroup::MulticastGroupPubAllowlistCommands::Add(
                                    args,
                                ) => args.execute(&client, &mut handle),
                                cli::multicastgroup::MulticastGroupPubAllowlistCommands::Remove(
                                    args,
                                ) => args.execute(&client, &mut handle),
                            }
                        }
                        cli::multicastgroup::MulticastGroupAllowlistCommands::Subscriber(args) => {
                            match args.command {
                                cli::multicastgroup::MulticastGroupSubAllowlistCommands::List(
                                    args,
                                ) => args.execute(&client, &mut handle),
                                cli::multicastgroup::MulticastGroupSubAllowlistCommands::Add(
                                    args,
                                ) => args.execute(&client, &mut handle),
                                cli::multicastgroup::MulticastGroupSubAllowlistCommands::Remove(
                                    args,
                                ) => args.execute(&client, &mut handle),
                            }
                        }
                    }
                }
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
            cli::multicast::MulticastCommands::Subscribe(args) => args.execute(&client).await,
            cli::multicast::MulticastCommands::Unsubscribe(args) => args.execute(&client).await,
            cli::multicast::MulticastCommands::Publish(args) => args.execute(&client).await,
            cli::multicast::MulticastCommands::Unpublish(args) => args.execute(&client).await,
        },

        Command::Geolocation(command) => {
            let geo_client =
                GeoClient::new(url.clone(), app.geo_program_id.clone(), app.keypair.clone())?;
            let svc_program_id = *dzclient.get_program_id();
            let (globalstate_pk, _) = get_globalstate_pda(&svc_program_id);
            let geo_cli = GeoCliCommandImpl::new(&geo_client, &dzclient, globalstate_pk);
            match command.command {
                GeolocationCommands::Probe(command) => match command.command {
                    ProbeCommands::Create(args) => args.execute(&geo_cli, &mut handle),
                    ProbeCommands::Update(args) => args.execute(&geo_cli, &mut handle),
                    ProbeCommands::Delete(args) => args.execute(&geo_cli, &mut handle),
                    ProbeCommands::Get(args) => args.execute(&geo_cli, &mut handle),
                    ProbeCommands::List(args) => args.execute(&geo_cli, &mut handle),
                    ProbeCommands::AddParent(args) => args.execute(&geo_cli, &mut handle),
                    ProbeCommands::RemoveParent(args) => args.execute(&geo_cli, &mut handle),
                },
                GeolocationCommands::User(command) => match command.command {
                    GeoUserCommands::Create(args) => args.execute(&geo_cli, &mut handle),
                    GeoUserCommands::Delete(args) => args.execute(&geo_cli, &mut handle),
                    GeoUserCommands::Update(args) => args.execute(&geo_cli, &mut handle),
                    GeoUserCommands::Get(args) => args.execute(&geo_cli, &mut handle),
                    GeoUserCommands::List(args) => args.execute(&geo_cli, &mut handle),
                    GeoUserCommands::AddTarget(args) => args.execute(&geo_cli, &mut handle),
                    GeoUserCommands::RemoveTarget(args) => args.execute(&geo_cli, &mut handle),
                    GeoUserCommands::SetResultDestination(args) => {
                        args.execute(&geo_cli, &mut handle)
                    }
                    GeoUserCommands::UpdatePayment(args) => args.execute(&geo_cli, &mut handle),
                },
            }
        }

        Command::Resource(command) => match command.command {
            cli::resource::ResourceCommands::Allocate(args) => args.execute(&client, &mut handle),
            cli::resource::ResourceCommands::Create(args) => args.execute(&client, &mut handle),
            cli::resource::ResourceCommands::Deallocate(args) => args.execute(&client, &mut handle),
            cli::resource::ResourceCommands::Get(args) => args.execute(&client, &mut handle),
            cli::resource::ResourceCommands::Close(args) => args.execute(&client, &mut handle),
            cli::resource::ResourceCommands::Verify(args) => args.execute(&client, &mut handle),
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
}
