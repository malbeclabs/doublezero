//! Top-level serviceability subcommand enum per RFC-20 §Module contract.
//!
//! The unified `doublezero` binary mounts this enum via `#[command(flatten)]`
//! so its variants surface as top-level commands (`doublezero device list`,
//! `doublezero location get`, ...). The binary keeps its own `Command` enum
//! for binary-local verbs (daemon-control, geolocation, completion, and the
//! raw-`DZClient` diagnostics like `Account`, `Accounts`, `Log`).

use clap::Subcommand;
use doublezero_cli_core::CliContext;
use std::io::Write;

use crate::{
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
        permission::{PermissionCliCommand, PermissionCommands},
        resource::{ResourceCliCommand, ResourceCommands},
        tenant::{AdministratorCommands, TenantCliCommand, TenantCommands},
        user::{UserCliCommand, UserCommands},
    },
    doublezerocommand::CliCommand,
    export::ExportCliCommand,
    init::InitCliCommand,
    keygen::KeyGenCliCommand,
    migrate::MigrateCliCommand,
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
            Self::Init(args) => args.execute(client, out),
            Self::Migrate(args) => args.execute(client, out),
            Self::Address(args) => args.execute(client, out),
            Self::Balance(args) => args.execute(client, out),
            Self::Export(args) => args.execute(client, out),
            Self::Keygen(args) => args.execute(client, out),

            Self::Config(cmd) => match cmd.command {
                ConfigCommands::Get(args) => args.execute(client, out),
                ConfigCommands::Set(args) => args.execute(client, out),
            },
            Self::GlobalConfig(cmd) => match cmd.command {
                GlobalConfigCommands::Set(args) => args.execute(client, out),
                GlobalConfigCommands::Get(args) => args.execute(client, out),
                GlobalConfigCommands::Airdrop(c) => match c.command {
                    AirdropCommands::Set(args) => args.execute(client, out),
                    AirdropCommands::Get(args) => args.execute(client, out),
                },
                GlobalConfigCommands::Authority(c) => match c.command {
                    AuthorityCommands::Set(args) => args.execute(client, out),
                    AuthorityCommands::Get(args) => args.execute(client, out),
                },
                GlobalConfigCommands::Allowlist(c) => match c.command {
                    FoundationAllowlistCommands::List(args) => args.execute(client, out),
                    FoundationAllowlistCommands::Add(args) => args.execute(client, out),
                    FoundationAllowlistCommands::Remove(args) => args.execute(client, out),
                },
                GlobalConfigCommands::QaAllowlist(c) => match c.command {
                    QaAllowlistCommands::List(args) => args.execute(client, out),
                    QaAllowlistCommands::Add(args) => args.execute(client, out),
                    QaAllowlistCommands::Remove(args) => args.execute(client, out),
                },
                GlobalConfigCommands::SetVersion(args) => args.execute(client, out),
                GlobalConfigCommands::FeatureFlags(c) => match c.command {
                    FeatureFlagsCommands::Get(args) => args.execute(client, out),
                    FeatureFlagsCommands::Set(args) => args.execute(client, out),
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
                ExchangeCommands::Create(args) => args.execute(client, out),
                ExchangeCommands::SetDevice(args) => args.execute(client, out),
                ExchangeCommands::Update(args) => args.execute(client, out),
                ExchangeCommands::List(args) => args.execute(client, out),
                ExchangeCommands::Get(args) => args.execute(client, out),
                ExchangeCommands::Delete(args) => args.execute(client, out),
            },
            Self::Contributor(cmd) => match cmd.command {
                ContributorCommands::Create(args) => args.execute(client, out),
                ContributorCommands::Update(args) => args.execute(client, out),
                ContributorCommands::List(args) => args.execute(client, out),
                ContributorCommands::Get(args) => args.execute(client, out),
                ContributorCommands::Delete(args) => args.execute(client, out),
            },
            Self::Permission(cmd) => match cmd.command {
                PermissionCommands::Set(args) => args.execute(client, out),
                PermissionCommands::Suspend(args) => args.execute(client, out),
                PermissionCommands::Resume(args) => args.execute(client, out),
                PermissionCommands::Delete(args) => args.execute(client, out),
                PermissionCommands::Get(args) => args.execute(client, out),
                PermissionCommands::List(args) => args.execute(client, out),
            },
            Self::Tenant(cmd) => match cmd.command {
                TenantCommands::Create(args) => args.execute(client, out),
                TenantCommands::Update(args) => args.execute(client, out),
                TenantCommands::List(args) => args.execute(client, out),
                TenantCommands::Get(args) => args.execute(client, out),
                TenantCommands::Delete(args) => args.execute(client, out),
                TenantCommands::Administrator(c) => match c.command {
                    AdministratorCommands::Add(args) => args.execute(client, out),
                    AdministratorCommands::Remove(args) => args.execute(client, out),
                },
            },
            Self::Device(cmd) => match cmd.command {
                DeviceCommands::Create(args) => args.execute(client, out),
                DeviceCommands::Update(args) => args.execute(client, out),
                DeviceCommands::List(args) => args.execute(client, out),
                DeviceCommands::Get(args) => args.execute(client, out),
                DeviceCommands::Delete(args) => args.execute(client, out),
                DeviceCommands::Interface(c) => match c.command {
                    InterfaceCommands::Create(args) => args.execute(client, out),
                    InterfaceCommands::Update(args) => args.execute(client, out),
                    InterfaceCommands::List(args) => args.execute(client, out),
                    InterfaceCommands::Get(args) => args.execute(client, out),
                    InterfaceCommands::Delete(args) => args.execute(client, out),
                },
                DeviceCommands::SetHealth(args) => args.execute(client, out),
            },
            Self::Link(cmd) => match cmd.command {
                LinkCommands::Create(args) => match args.command {
                    CreateLinkCommands::Wan(args) => args.execute(client, out),
                    CreateLinkCommands::Dzx(args) => args.execute(client, out),
                },
                LinkCommands::Accept(args) => args.execute(client, out),
                LinkCommands::Update(args) => args.execute(client, out),
                LinkCommands::List(args) => args.execute(client, out),
                LinkCommands::Get(args) => args.execute(client, out),
                LinkCommands::Latency(args) => args.execute(client, out),
                LinkCommands::Delete(args) => args.execute(client, out),
                LinkCommands::SetHealth(args) => args.execute(client, out),
                LinkCommands::Topology(t) => match t.command {
                    TopologyCommands::Create(args) => args.execute(client, out),
                    TopologyCommands::Delete(args) => args.execute(client, out),
                    TopologyCommands::Clear(args) => args.execute(client, out),
                    TopologyCommands::AssignNodeSegments(args) => args.execute(client, out),
                    TopologyCommands::List(args) => args.execute(client, out),
                },
            },
            Self::AccessPass(cmd) => match cmd.command {
                AccessPassCommands::Set(args) => args.execute(client, out),
                AccessPassCommands::Close(args) => args.execute(client, out),
                AccessPassCommands::List(args) => args.execute(client, out),
                AccessPassCommands::Get(args) => args.execute(client, out),
                AccessPassCommands::UserBalances(args) => args.execute(client, out),
                AccessPassCommands::Fund(args) => {
                    args.execute(client, out, &mut std::io::stdin().lock())
                }
            },
            Self::User(cmd) => match cmd.command {
                UserCommands::Create(args) => args.execute(client, out),
                UserCommands::CreateSubscribe(args) => args.execute(client, out),
                UserCommands::Subscribe(args) => args.execute(client, out),
                UserCommands::Update(args) => args.execute(client, out),
                UserCommands::List(args) => args.execute(client, out),
                UserCommands::Get(args) => args.execute(client, out),
                UserCommands::Delete(args) => args.execute(client, out),
                UserCommands::RequestBan(args) => args.execute(client, out),
            },
            Self::Resource(cmd) => match cmd.command {
                ResourceCommands::Allocate(args) => args.execute(client, out),
                ResourceCommands::Create(args) => args.execute(client, out),
                ResourceCommands::Deallocate(args) => args.execute(client, out),
                ResourceCommands::Get(args) => args.execute(client, out),
                ResourceCommands::Close(args) => args.execute(client, out),
                ResourceCommands::Verify(args) => args.execute(client, out),
            },
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

    // `hide = true` must not gate parsing - operators and automation rely on
    // these verbs being reachable even though they do not appear in --help.
    #[test]
    fn parses_hidden_init() {
        let parsed = TestCli::try_parse_from(["test", "init"]).unwrap();
        assert!(matches!(parsed.command, ServiceabilityCommand::Init(_)));
    }

    #[test]
    fn parses_hidden_migrate() {
        let parsed = TestCli::try_parse_from(["test", "migrate"]).unwrap();
        assert!(matches!(parsed.command, ServiceabilityCommand::Migrate(_)));
    }
}
