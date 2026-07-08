//! Top-level serviceability subcommand enum per RFC-20 §Module contract.
//!
//! The unified `doublezero` binary mounts this enum via `#[command(flatten)]`
//! so its variants surface as top-level commands (`doublezero device list`,
//! `doublezero location get`, ...). The binary keeps its own `Command` enum
//! for binary-local verbs (daemon-control, geolocation, completion).

use clap::Subcommand;
use doublezero_cli_core::CliContext;
use std::io::Write;

use crate::{
    account::GetAccountCliCommand,
    accounts::GetAccountsCliCommand,
    address::AddressCliCommand,
    balance::BalanceCliCommand,
    cli::{
        accesspass::{AccessPassCliCommand, AccessPassCommands},
        config::{ConfigCliCommand, ConfigCommands},
        contributor::{ContributorCliCommand, ContributorCommands},
        device::{DeviceCliCommand, DeviceCommands, InterfaceCommands},
        exchange::{ExchangeCliCommand, ExchangeCommands},
        globalconfig::{
            AirdropCommands, AuthorityCommands, FeatureFlagsCommands, FoundationAllowlistCommands,
            GlobalConfigCliCommand, GlobalConfigCommands, QaAllowlistCommands,
        },
        link::{CreateLinkCommands, LinkCliCommand, LinkCommands, TopologyCommands},
        location::{LocationCliCommand, LocationCommands},
        migrate::{MigrateCliCommand, MigrateCommands},
        permission::{PermissionCliCommand, PermissionCommands},
        resource::{ResourceCliCommand, ResourceCommands},
        tenant::{AdministratorCommands, TenantCliCommand, TenantCommands},
        user::{UserCliCommand, UserCommands},
    },
    doublezerocommand::CliCommand,
    export::ExportCliCommand,
    init::InitCliCommand,
    keygen::KeyGenCliCommand,
    logcommand::LogCliCommand,
    subscribe::SubscribeCliCommand,
    version::VersionCliCommand,
};

#[derive(Subcommand, Debug)]
pub enum ServiceabilityCommand {
    #[command(hide = true)]
    Init(InitCliCommand),
    #[command(hide = true)]
    Migrate(MigrateCliCommand),

    /// Get your public key
    Address(AddressCliCommand),
    /// Get your balance
    Balance(BalanceCliCommand),

    /// local configuration
    Config(ConfigCliCommand),
    /// Global network configuration
    GlobalConfig(GlobalConfigCliCommand),

    /// Manage locations
    Location(LocationCliCommand),
    /// Manage exchanges
    Exchange(ExchangeCliCommand),
    /// Manage contributors
    Contributor(ContributorCliCommand),
    /// Manage permissions
    Permission(PermissionCliCommand),
    /// Manage tenants
    Tenant(TenantCliCommand),
    /// Manage devices
    Device(DeviceCliCommand),
    /// Manage tunnels between devices
    Link(LinkCliCommand),
    /// Manage access passes
    AccessPass(AccessPassCliCommand),
    /// Manage users
    User(UserCliCommand),

    /// Export all data to files
    Export(ExportCliCommand),
    /// Create a new user identity
    Keygen(KeyGenCliCommand),

    /// IP/ID Resource Management
    Resource(ResourceCliCommand),

    /// Print version information
    Version(VersionCliCommand),
    /// Get Account
    Account(GetAccountCliCommand),
    /// List Accounts
    #[command(hide = true)]
    Accounts(GetAccountsCliCommand),
    /// Get logs
    Log(LogCliCommand),
    /// Subscribe to program events
    #[command(hide = true)]
    Subscribe(SubscribeCliCommand),
}

impl ServiceabilityCommand {
    /// Dispatch a serviceability verb to its implementation.
    ///
    /// `ctx` is forwarded to every verb whose signature accepts it. As more
    /// verbs migrate to the RFC-20 `async fn execute(self, ctx, client, out)`
    /// shape, additional arms below await their futures directly.
    pub async fn execute<C, W>(self, ctx: &CliContext, client: &C, out: &mut W) -> eyre::Result<()>
    where
        C: CliCommand,
        W: Write,
    {
        match self {
            Self::Init(args) => args.execute(ctx, client, out).await,
            Self::Migrate(args) => match args.command {
                MigrateCommands::UserPda(cmd) => cmd.execute(ctx, client, out).await,
                MigrateCommands::FlexAlgo(cmd) => cmd.execute(ctx, client, out).await,
            },
            Self::Address(args) => args.execute(ctx, client, out).await,
            Self::Balance(args) => args.execute(ctx, client, out).await,
            Self::Export(args) => args.execute(ctx, client, out).await,
            Self::Keygen(args) => args.execute(ctx, client, out).await,

            Self::Config(cmd) => match cmd.command {
                ConfigCommands::Get(args) => args.execute(ctx, client, out).await,
                ConfigCommands::Set(args) => args.execute(ctx, client, out).await,
            },
            Self::GlobalConfig(cmd) => match cmd.command {
                GlobalConfigCommands::Set(args) => args.execute(ctx, client, out).await,
                GlobalConfigCommands::Get(args) => args.execute(ctx, client, out).await,
                GlobalConfigCommands::Airdrop(c) => match c.command {
                    AirdropCommands::Set(args) => args.execute(ctx, client, out).await,
                    AirdropCommands::Get(args) => args.execute(ctx, client, out).await,
                },
                GlobalConfigCommands::Authority(c) => match c.command {
                    AuthorityCommands::Set(args) => args.execute(ctx, client, out).await,
                    AuthorityCommands::Get(args) => args.execute(ctx, client, out).await,
                },
                GlobalConfigCommands::Allowlist(c) => match c.command {
                    FoundationAllowlistCommands::List(args) => args.execute(ctx, client, out).await,
                    FoundationAllowlistCommands::Add(args) => args.execute(ctx, client, out).await,
                    FoundationAllowlistCommands::Remove(args) => {
                        args.execute(ctx, client, out).await
                    }
                },
                GlobalConfigCommands::QaAllowlist(c) => match c.command {
                    QaAllowlistCommands::List(args) => args.execute(ctx, client, out).await,
                    QaAllowlistCommands::Add(args) => args.execute(ctx, client, out).await,
                    QaAllowlistCommands::Remove(args) => args.execute(ctx, client, out).await,
                },
                GlobalConfigCommands::SetVersion(args) => args.execute(ctx, client, out).await,
                GlobalConfigCommands::FeatureFlags(c) => match c.command {
                    FeatureFlagsCommands::Get(args) => args.execute(ctx, client, out).await,
                    FeatureFlagsCommands::Set(args) => args.execute(ctx, client, out).await,
                },
            },

            Self::Location(cmd) => match cmd.command {
                LocationCommands::Create(args) => args.execute(ctx, client, out).await,
                LocationCommands::Update(args) => args.execute(ctx, client, out).await,
                LocationCommands::List(args) => args.execute(ctx, client, out).await,
                LocationCommands::Get(args) => args.execute(ctx, client, out).await,
                LocationCommands::Delete(args) => args.execute(ctx, client, out).await,
            },
            Self::Exchange(cmd) => match cmd.command {
                ExchangeCommands::Create(args) => args.execute(ctx, client, out).await,
                ExchangeCommands::SetDevice(args) => args.execute(ctx, client, out).await,
                ExchangeCommands::Update(args) => args.execute(ctx, client, out).await,
                ExchangeCommands::List(args) => args.execute(ctx, client, out).await,
                ExchangeCommands::Get(args) => args.execute(ctx, client, out).await,
                ExchangeCommands::Delete(args) => args.execute(ctx, client, out).await,
            },
            Self::Contributor(cmd) => match cmd.command {
                ContributorCommands::Create(args) => args.execute(ctx, client, out).await,
                ContributorCommands::Update(args) => args.execute(ctx, client, out).await,
                ContributorCommands::List(args) => args.execute(ctx, client, out).await,
                ContributorCommands::Get(args) => args.execute(ctx, client, out).await,
                ContributorCommands::Delete(args) => args.execute(ctx, client, out).await,
            },
            Self::Permission(cmd) => match cmd.command {
                PermissionCommands::Set(args) => args.execute(ctx, client, out).await,
                PermissionCommands::Suspend(args) => args.execute(ctx, client, out).await,
                PermissionCommands::Resume(args) => args.execute(ctx, client, out).await,
                PermissionCommands::Delete(args) => args.execute(ctx, client, out).await,
                PermissionCommands::Get(args) => args.execute(ctx, client, out).await,
                PermissionCommands::List(args) => args.execute(ctx, client, out).await,
                PermissionCommands::Audit(args) => args.execute(ctx, client, out).await,
            },
            Self::Tenant(cmd) => match cmd.command {
                TenantCommands::Create(args) => args.execute(ctx, client, out).await,
                TenantCommands::Update(args) => args.execute(ctx, client, out).await,
                TenantCommands::List(args) => args.execute(ctx, client, out).await,
                TenantCommands::Get(args) => args.execute(ctx, client, out).await,
                TenantCommands::Delete(args) => args.execute(ctx, client, out).await,
                TenantCommands::Administrator(c) => match c.command {
                    AdministratorCommands::Add(args) => args.execute(ctx, client, out).await,
                    AdministratorCommands::Remove(args) => args.execute(ctx, client, out).await,
                },
            },
            Self::Device(cmd) => match cmd.command {
                DeviceCommands::Create(args) => args.execute(ctx, client, out).await,
                DeviceCommands::Update(args) => args.execute(ctx, client, out).await,
                DeviceCommands::List(args) => args.execute(ctx, client, out).await,
                DeviceCommands::Get(args) => args.execute(ctx, client, out).await,
                DeviceCommands::Delete(args) => args.execute(ctx, client, out).await,
                DeviceCommands::Interface(c) => match c.command {
                    InterfaceCommands::Create(args) => args.execute(ctx, client, out).await,
                    InterfaceCommands::Update(args) => args.execute(ctx, client, out).await,
                    InterfaceCommands::List(args) => args.execute(ctx, client, out).await,
                    InterfaceCommands::Get(args) => args.execute(ctx, client, out).await,
                    InterfaceCommands::Delete(args) => args.execute(ctx, client, out).await,
                },
                DeviceCommands::SetHealth(args) => args.execute(ctx, client, out).await,
                DeviceCommands::MigrateMulticastCounts(args) => {
                    args.execute(ctx, client, out).await
                }
                DeviceCommands::MigrateUnicastCounts(args) => args.execute(ctx, client, out).await,
            },
            Self::Link(cmd) => match cmd.command {
                LinkCommands::Create(args) => match args.command {
                    CreateLinkCommands::Wan(args) => args.execute(ctx, client, out).await,
                    CreateLinkCommands::Dzx(args) => args.execute(ctx, client, out).await,
                },
                LinkCommands::Accept(args) => args.execute(ctx, client, out).await,
                LinkCommands::Update(args) => args.execute(ctx, client, out).await,
                LinkCommands::List(args) => args.execute(ctx, client, out).await,
                LinkCommands::Get(args) => args.execute(ctx, client, out).await,
                LinkCommands::Latency(args) => args.execute(ctx, client, out).await,
                LinkCommands::Delete(args) => args.execute(ctx, client, out).await,
                LinkCommands::SetHealth(args) => args.execute(ctx, client, out).await,
                LinkCommands::Topology(t) => match t.command {
                    TopologyCommands::Create(args) => args.execute(ctx, client, out).await,
                    TopologyCommands::Delete(args) => args.execute(ctx, client, out).await,
                    TopologyCommands::Clear(args) => args.execute(ctx, client, out).await,
                    TopologyCommands::AssignNodeSegments(args) => {
                        args.execute(ctx, client, out).await
                    }
                    TopologyCommands::List(args) => args.execute(ctx, client, out).await,
                },
            },
            Self::AccessPass(cmd) => match cmd.command {
                AccessPassCommands::Set(args) => args.execute(ctx, client, out).await,
                AccessPassCommands::Close(args) => args.execute(ctx, client, out).await,
                AccessPassCommands::List(args) => args.execute(ctx, client, out).await,
                AccessPassCommands::Get(args) => args.execute(ctx, client, out).await,
                AccessPassCommands::UserBalances(args) => args.execute(ctx, client, out).await,
                AccessPassCommands::Fund(args) => {
                    args.execute(ctx, client, out, &mut std::io::stdin().lock())
                        .await
                }
            },
            Self::User(cmd) => match cmd.command {
                UserCommands::Create(args) => args.execute(ctx, client, out).await,
                UserCommands::CreateSubscribe(args) => args.execute(ctx, client, out).await,
                UserCommands::Subscribe(args) => args.execute(ctx, client, out).await,
                UserCommands::Update(args) => args.execute(ctx, client, out).await,
                UserCommands::List(args) => args.execute(ctx, client, out).await,
                UserCommands::Get(args) => args.execute(ctx, client, out).await,
                UserCommands::Delete(args) => args.execute(ctx, client, out).await,
                UserCommands::RequestBan(args) => args.execute(ctx, client, out).await,
            },
            Self::Resource(cmd) => match cmd.command {
                ResourceCommands::Allocate(args) => args.execute(ctx, client, out).await,
                ResourceCommands::Create(args) => args.execute(ctx, client, out).await,
                ResourceCommands::Deallocate(args) => args.execute(ctx, client, out).await,
                ResourceCommands::Get(args) => args.execute(ctx, client, out).await,
                ResourceCommands::Close(args) => args.execute(ctx, client, out).await,
                ResourceCommands::Verify(args) => args.execute(ctx, client, out).await,
            },

            Self::Version(args) => args.execute(ctx, client, out).await,
            Self::Account(args) => args.execute(ctx, client, out).await,
            Self::Accounts(args) => args.execute(ctx, client, out).await,
            Self::Log(args) => args.execute(ctx, client, out).await,
            Self::Subscribe(args) => args.execute(ctx, client, out).await,
        }
    }
}

#[cfg(test)]
mod tests {
    //! Parse-level parity tests for `ServiceabilityCommand`.
    //!
    //! Once the unified `doublezero` binary mounts this enum via
    //! `#[command(flatten)]`, the variant tree below becomes the user-facing
    //! parse tree for every serviceability verb. A wrong `Subcommand` attribute
    //! or a misrouted nested enum here would land directly in production, so we
    //! pin the representative chains to specific variants.
    //!
    //! The tests cover parse-time routing only - they do not invoke `execute`.
    //! Per-verb behavior is covered by inline tests next to each leaf command.
    use super::*;
    use crate::cli::{device::InterfaceCliCommand, link::CreateLinkCommand};
    use clap::Parser;

    #[derive(Parser, Debug)]
    struct TestCli {
        #[command(subcommand)]
        command: ServiceabilityCommand,
    }

    /// The system program id: a syntactically valid 32-byte base58 pubkey.
    /// Used wherever a verb takes an identifier validated by
    /// `validate_pubkey_or_code`.
    const TEST_PUBKEY: &str = "11111111111111111111111111111111";

    #[test]
    fn parses_location_get() {
        let parsed =
            TestCli::try_parse_from(["test", "location", "get", "--code", TEST_PUBKEY]).unwrap();
        assert!(matches!(
            parsed.command,
            ServiceabilityCommand::Location(LocationCliCommand {
                command: LocationCommands::Get(_),
            })
        ));
    }

    #[test]
    fn parses_device_interface_get() {
        let parsed = TestCli::try_parse_from([
            "test",
            "device",
            "interface",
            "get",
            TEST_PUBKEY,
            "Ethernet1",
        ])
        .unwrap();
        assert!(matches!(
            parsed.command,
            ServiceabilityCommand::Device(DeviceCliCommand {
                command: DeviceCommands::Interface(InterfaceCliCommand {
                    command: InterfaceCommands::Get(_),
                }),
            })
        ));
    }

    #[test]
    fn parses_link_create_wan() {
        let parsed = TestCli::try_parse_from([
            "test",
            "link",
            "create",
            "wan",
            "--code",
            "test-link",
            "--contributor",
            TEST_PUBKEY,
            "--side-a",
            TEST_PUBKEY,
            "--side-a-interface",
            "Ethernet1",
            "--side-z",
            TEST_PUBKEY,
            "--side-z-interface",
            "Ethernet2",
            "--bandwidth",
            "1Gbps",
            "--delay-ms",
            "5",
            "--jitter-ms",
            "1",
        ])
        .unwrap();
        assert!(matches!(
            parsed.command,
            ServiceabilityCommand::Link(LinkCliCommand {
                command: LinkCommands::Create(CreateLinkCommand {
                    command: CreateLinkCommands::Wan(_),
                }),
            })
        ));
    }

    #[test]
    fn parses_access_pass_fund() {
        let parsed = TestCli::try_parse_from(["test", "access-pass", "fund"]).unwrap();
        assert!(matches!(
            parsed.command,
            ServiceabilityCommand::AccessPass(AccessPassCliCommand {
                command: AccessPassCommands::Fund(_),
            })
        ));
    }

    #[test]
    fn parses_resource_verify() {
        let parsed = TestCli::try_parse_from(["test", "resource", "verify"]).unwrap();
        assert!(matches!(
            parsed.command,
            ServiceabilityCommand::Resource(ResourceCliCommand {
                command: ResourceCommands::Verify(_),
            })
        ));
    }

    #[test]
    fn parses_permission_audit() {
        let parsed = TestCli::try_parse_from(["test", "permission", "audit"]).unwrap();
        assert!(matches!(
            parsed.command,
            ServiceabilityCommand::Permission(PermissionCliCommand {
                command: PermissionCommands::Audit(_),
            })
        ));
    }

    // `hide = true` must not gate parsing - operators and automation rely on
    // these verbs being reachable even though they do not appear in --help.
    #[test]
    fn parses_hidden_init() {
        let parsed = TestCli::try_parse_from(["test", "init"]).unwrap();
        assert!(matches!(parsed.command, ServiceabilityCommand::Init(_)));
    }

    // Bare `migrate` now requires a subcommand; assert the two leaves route.
    #[test]
    fn parses_hidden_migrate_user_pda() {
        let parsed = TestCli::try_parse_from(["test", "migrate", "user-pda"]).unwrap();
        assert!(matches!(
            parsed.command,
            ServiceabilityCommand::Migrate(MigrateCliCommand {
                command: MigrateCommands::UserPda(_),
            })
        ));
    }

    #[test]
    fn parses_hidden_migrate_flex_algo() {
        let parsed =
            TestCli::try_parse_from(["test", "migrate", "flex-algo", "--dry-run"]).unwrap();
        assert!(matches!(
            parsed.command,
            ServiceabilityCommand::Migrate(MigrateCliCommand {
                command: MigrateCommands::FlexAlgo(_),
            })
        ));
    }

    #[test]
    fn parses_hidden_device_migrate_counts() {
        let multicast =
            TestCli::try_parse_from(["test", "device", "migrate-multicast-counts"]).unwrap();
        assert!(matches!(
            multicast.command,
            ServiceabilityCommand::Device(DeviceCliCommand {
                command: DeviceCommands::MigrateMulticastCounts(_),
            })
        ));

        let unicast =
            TestCli::try_parse_from(["test", "device", "migrate-unicast-counts", "--dry-run"])
                .unwrap();
        assert!(matches!(
            unicast.command,
            ServiceabilityCommand::Device(DeviceCliCommand {
                command: DeviceCommands::MigrateUnicastCounts(_),
            })
        ));
    }

    #[test]
    fn parses_version() {
        let parsed = TestCli::try_parse_from(["test", "version"]).unwrap();
        assert!(matches!(parsed.command, ServiceabilityCommand::Version(_)));
    }

    #[test]
    fn parses_account() {
        let parsed = TestCli::try_parse_from(["test", "account", "--pubkey", TEST_PUBKEY]).unwrap();
        assert!(matches!(parsed.command, ServiceabilityCommand::Account(_)));
    }

    #[test]
    fn parses_hidden_accounts() {
        let parsed = TestCli::try_parse_from(["test", "accounts"]).unwrap();
        assert!(matches!(parsed.command, ServiceabilityCommand::Accounts(_)));
    }

    #[test]
    fn parses_log() {
        let parsed = TestCli::try_parse_from(["test", "log", "--pubkey", TEST_PUBKEY]).unwrap();
        assert!(matches!(parsed.command, ServiceabilityCommand::Log(_)));
    }

    #[test]
    fn parses_hidden_subscribe() {
        let parsed = TestCli::try_parse_from(["test", "subscribe"]).unwrap();
        assert!(matches!(
            parsed.command,
            ServiceabilityCommand::Subscribe(_)
        ));
    }
}
