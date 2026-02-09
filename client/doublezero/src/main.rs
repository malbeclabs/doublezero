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
    globalconfig::{
        AirdropCommands, AuthorityCommands, FoundationAllowlistCommands, GlobalConfigCommands,
        QaAllowlistCommands,
    },
    link::LinkCommands,
    location::LocationCommands,
    user::UserCommands,
};
use doublezero_cli::{checkversion::check_version, doublezerocommand::CliCommandImpl};
use doublezero_sdk::{DZClient, ProgramVersion};

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(name = "DoubleZero")]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "DoubleZero client tool", long_about = None)]
struct App {
    #[command(subcommand)]
    command: Command,
    /// DZ env (testnet, devnet, or mainnet-beta)
    #[arg(short, long, value_name = "ENV", global = true)]
    env: Option<String>,
    /// DZ ledger RPC URL
    #[arg(long, value_name = "RPC_URL", global = true)]
    url: Option<String>,
    /// DZ ledger WebSocket URL
    #[arg(long, value_name = "WEBSOCKET_URL", global = true)]
    ws: Option<String>,
    /// DZ program ID (testnet or devnet)
    #[arg(long, value_name = "PROGRAM_ID", global = true)]
    program_id: Option<String>,
    /// Path to the keypair file
    #[arg(long, value_name = "KEYPAIR", global = true)]
    keypair: Option<PathBuf>,
    /// Suppress version warning output
    #[arg(long, global = true)]
    no_version_warning: bool,
}

#[tokio::main]
async fn main() -> eyre::Result<()> {
    let app = App::parse();

    if let Some(keypair) = &app.keypair {
        println!("using keypair: {}", keypair.display());
    }

    let (url, ws, program_id) = if let Some(env) = app.env {
        let config = match env.parse::<Environment>() {
            Ok(env) => match env.config() {
                Ok(config) => config,
                Err(e) => {
                    eprintln!("Error: {e}");
                    std::process::exit(1);
                }
            },
            Err(e) => {
                eprintln!("Error: {e}");
                std::process::exit(1);
            }
        };
        (
            Some(config.ledger_public_rpc_url),
            Some(config.ledger_public_ws_rpc_url),
            Some(config.serviceability_program_id.to_string()),
        )
    } else {
        (app.url, app.ws, app.program_id)
    };

    let dzclient = DZClient::new(url, ws, program_id, app.keypair)?;
    let client = CliCommandImpl::new(&dzclient);

    let stdout = std::io::stdout();
    let mut handle = stdout.lock();

    // Skip version check for Status command to allow checking status of services when the program is running
    if !app.no_version_warning
        && !matches!(app.command, Command::Status(_))
        && !matches!(app.command, Command::Address(_))
        && !matches!(app.command, Command::Balance(_))
        && !matches!(app.command, Command::Export(_))
        && !matches!(app.command, Command::Completion(_))
    {
        let stderr = std::io::stderr();
        let mut err_handle = stderr.lock();
        check_version(&client, &mut err_handle, ProgramVersion::current())?;
    }

    let res = match app.command {
        Command::Address(args) => args.execute(&client, &mut handle),
        Command::Balance(args) => args.execute(&client, &mut handle),
        Command::Connect(args) => args.execute(&client).await,
        Command::Status(args) => args.execute(&client).await,
        Command::Disconnect(args) => args.execute(&client).await,
        Command::Latency(args) => args.execute(&client).await,
        Command::Routes(args) => args.execute(&client).await,

        Command::Init(args) => args.execute(&client, &mut handle),
        Command::Migrate(args) => args.execute(&client, &mut handle),
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
        },
        Command::Account(args) => args.execute(&dzclient, &mut handle),
        Command::Accounts(args) => args.execute(&dzclient, &mut handle),
        Command::Location(command) => match command.command {
            LocationCommands::Create(args) => args.execute(&client, &mut handle),
            LocationCommands::Update(args) => args.execute(&client, &mut handle),
            LocationCommands::List(args) => args.execute(&client, &mut handle),
            LocationCommands::Get(args) => args.execute(&client, &mut handle),
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
        Command::Tenant(command) => match command.command {
            cli::tenant::TenantCommands::Create(args) => args.execute(&client, &mut handle),
            cli::tenant::TenantCommands::Update(args) => args.execute(&client, &mut handle),
            cli::tenant::TenantCommands::List(args) => args.execute(&client, &mut handle),
            cli::tenant::TenantCommands::Get(args) => args.execute(&client, &mut handle),
            cli::tenant::TenantCommands::Delete(args) => args.execute(&client, &mut handle),
            cli::tenant::TenantCommands::AddAdministrator(args) => {
                args.execute(&client, &mut handle)
            }
            cli::tenant::TenantCommands::RemoveAdministrator(args) => {
                args.execute(&client, &mut handle)
            }
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
        },
        Command::AccessPass(command) => match command.command {
            cli::accesspass::AccessPassCommands::Set(args) => args.execute(&client, &mut handle),
            cli::accesspass::AccessPassCommands::Close(args) => args.execute(&client, &mut handle),
            cli::accesspass::AccessPassCommands::List(args) => args.execute(&client, &mut handle),
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
        },

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
            eprintln!("Error: {e}");
            std::process::exit(1);
        }
    };

    Ok(())
}
