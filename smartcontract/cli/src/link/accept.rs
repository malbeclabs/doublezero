use crate::{
    doublezerocommand::CliCommand,
    poll_for_activation::poll_for_link_activated,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_program_common::validate_iface;
use doublezero_sdk::{
    commands::{
        device::get::GetDeviceCommand,
        link::{accept::AcceptLinkCommand, get::GetLinkCommand},
    },
    InterfaceStatus, InterfaceType,
};
use eyre::eyre;
use std::io::Write;

#[derive(Args, Debug)]
pub struct AcceptLinkCliCommand {
    /// Link Pubkey or code to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
    /// Device interface name for side Z.
    #[arg(long, value_parser = validate_iface)]
    pub side_z_interface: String,
    /// Wait for the device to be activated
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl AcceptLinkCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, _) = client.get_link(GetLinkCommand {
            pubkey_or_code: self.code.clone(),
        })?;

        let (_, link) = client.get_link(GetLinkCommand {
            pubkey_or_code: pubkey.to_string(),
        })?;

        let (_, device_a) = client.get_device(GetDeviceCommand {
            pubkey_or_code: link.side_a_pk.to_string(),
        })?;

        let side_a_iface = device_a
            .interfaces
            .iter()
            .find(|i| i.name.to_lowercase() == link.side_a_iface_name.to_lowercase())
            .ok_or_else(|| {
                eyre!(
                    "Interface '{}' not found on side A device",
                    link.side_a_iface_name
                )
            })?;

        // Re-validate side A bandwidth at accept time. It was checked at link create,
        // but a contributor may have lowered it via device interface update between
        // create and accept; the same invariant is enforced onchain.
        if side_a_iface.bandwidth < link.bandwidth {
            return Err(eyre!(
                "Interface '{}' on side A device has bandwidth {} which is less than link bandwidth {}",
                link.side_a_iface_name, side_a_iface.bandwidth, link.bandwidth
            ));
        }

        let (_, device_z) = client.get_device(GetDeviceCommand {
            pubkey_or_code: link.side_z_pk.to_string(),
        })?;

        let side_z_iface = device_z
            .interfaces
            .iter()
            .find(|i| i.name.to_lowercase() == self.side_z_interface.to_lowercase())
            .ok_or_else(|| {
                eyre!(
                    "Interface '{}' not found on side Z device",
                    self.side_z_interface
                )
            })?;

        if side_z_iface.interface_type != InterfaceType::Physical {
            return Err(eyre!(
                "Interface '{}' on side Z device must be a physical interface",
                self.side_z_interface
            ));
        }

        if side_z_iface.status != InterfaceStatus::Unlinked {
            return Err(eyre!(
                "Interface '{}' on side Z device must be unlinked",
                self.side_z_interface
            ));
        }

        if side_z_iface.bandwidth < link.bandwidth {
            return Err(eyre!(
                "Interface '{}' on side Z device has bandwidth {} which is less than link bandwidth {}",
                self.side_z_interface, side_z_iface.bandwidth, link.bandwidth
            ));
        }

        let signature = client.accept_link(AcceptLinkCommand {
            link_pubkey: pubkey,
            side_z_iface_name: self.side_z_interface.clone(),
        })?;
        writeln!(out, "Signature: {signature}",)?;

        if self.wait {
            let device = poll_for_link_activated(client, &pubkey)?;
            writeln!(out, "Status: {0}", device.status)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};

    use crate::{
        doublezerocommand::{CliCommand, MockCliCommand},
        link::accept::AcceptLinkCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::{
            contributor::get::GetContributorCommand,
            device::get::GetDeviceCommand,
            link::{accept::AcceptLinkCommand, get::GetLinkCommand},
        },
        get_link_pda, AccountType, Contributor, ContributorStatus, Device, DeviceStatus,
        DeviceType, Interface, InterfaceStatus, Link, LinkLinkType, LinkStatus,
    };
    use doublezero_serviceability::state::interface::InterfaceType;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    const SIDE_A_IFACE: &str = "Ethernet1/1";
    const SIDE_Z_IFACE: &str = "Ethernet1/2";

    struct AcceptFixture {
        client: MockCliCommand,
        link_pubkey: Pubkey,
    }

    /// Build a mock CLI client wired up for `link accept`, with configurable
    /// side A / side Z interface bandwidths and link bandwidth. The link is in
    /// `Requested` status. Callers that want to exercise the happy path must
    /// also set `expect_accept_link` on the returned client.
    fn make_accept_fixture(
        side_a_bandwidth: u64,
        side_z_bandwidth: u64,
        link_bandwidth: u64,
    ) -> AcceptFixture {
        let mut client = create_test_client();

        let contributor_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcd");
        let contributor = Contributor {
            account_type: AccountType::Contributor,
            owner: Pubkey::default(),
            bump_seed: 255,
            reference_count: 0,
            index: 1,
            status: ContributorStatus::Activated,
            code: "co01".to_string(),
            ops_manager_pk: Pubkey::default(),
        };
        let (link_pubkey, _bump) = get_link_pda(&client.get_program_id(), 1);

        let device1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb");
        let device1 = Device {
            account_type: AccountType::Device,
            code: "dev01".to_string(),
            interfaces: vec![Interface {
                name: SIDE_A_IFACE.to_string(),
                status: InterfaceStatus::Unlinked,
                interface_type: InterfaceType::Physical,
                bandwidth: side_a_bandwidth,
                ..Default::default()
            }],
            device_type: DeviceType::Hybrid,
            public_ip: "127.0.0.1".parse().unwrap(),
            status: DeviceStatus::Activated,
            dz_prefixes: "10.0.0.1/32".parse().unwrap(),
            contributor_pk,
            mgmt_vrf: "default".to_string(),
            max_users: 255,
            ..Default::default()
        };
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device1_pk.to_string(),
            }))
            .returning(move |_| Ok((device1_pk, device1.clone())));

        let device2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf");
        let device2 = Device {
            account_type: AccountType::Device,
            code: "dev02".to_string(),
            interfaces: vec![Interface {
                status: InterfaceStatus::Unlinked,
                name: SIDE_Z_IFACE.to_string(),
                interface_type: InterfaceType::Physical,
                bandwidth: side_z_bandwidth,
                ip_net: "10.0.0.1/32".parse().unwrap(),
                ..Default::default()
            }],
            device_type: DeviceType::Hybrid,
            public_ip: "127.0.0.1".parse().unwrap(),
            status: DeviceStatus::Activated,
            dz_prefixes: "10.0.0.1/32".parse().unwrap(),
            contributor_pk,
            mgmt_vrf: "default".to_string(),
            max_users: 255,
            ..Default::default()
        };
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device2_pk.to_string(),
            }))
            .returning(move |_| Ok((device2_pk, device2.clone())));

        let tunnel = Link {
            account_type: AccountType::Link,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            contributor_pk,
            side_a_pk: device1_pk,
            side_z_pk: device2_pk,
            link_type: LinkLinkType::WAN,
            bandwidth: link_bandwidth,
            mtu: 9000,
            delay_ns: 10000000000,
            jitter_ns: 5000000000,
            delay_override_ns: 0,
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/16".parse().unwrap(),
            status: LinkStatus::Requested,
            owner: link_pubkey,
            side_a_iface_name: SIDE_A_IFACE.to_string(),
            side_z_iface_name: SIDE_Z_IFACE.to_string(),
            link_health: doublezero_serviceability::state::link::LinkHealth::ReadyForService,
            desired_status: doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
            link_topologies: vec![],
            link_flags: 0,
        };

        client
            .expect_get_contributor()
            .with(predicate::eq(GetContributorCommand {
                pubkey_or_code: contributor_pk.to_string(),
            }))
            .returning(move |_| Ok((contributor_pk, contributor.clone())));
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_link()
            .with(predicate::eq(GetLinkCommand {
                pubkey_or_code: link_pubkey.to_string(),
            }))
            .returning(move |_| Ok((link_pubkey, tunnel.clone())));

        AcceptFixture {
            client,
            link_pubkey,
        }
    }

    #[test]
    fn test_cli_link_accept() {
        // Both interfaces at 10 Gbps, link at 1 Gbps -> accept succeeds.
        let AcceptFixture {
            mut client,
            link_pubkey,
        } = make_accept_fixture(10_000_000_000, 10_000_000_000, 1_000_000_000);

        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);
        client
            .expect_accept_link()
            .with(predicate::eq(AcceptLinkCommand {
                link_pubkey,
                side_z_iface_name: SIDE_Z_IFACE.to_string(),
            }))
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();

        let mut output = Vec::new();
        let res = block_on(
            AcceptLinkCliCommand {
                code: link_pubkey.to_string(),
                side_z_interface: SIDE_Z_IFACE.to_string(),
                wait: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "Error: {}", res.unwrap_err());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            "Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_link_accept_rejects_insufficient_side_a_bandwidth() {
        // Side A interface at 500 Mbps cannot accept a 1 Gbps link.
        let AcceptFixture {
            client,
            link_pubkey,
            ..
        } = make_accept_fixture(500_000_000, 10_000_000_000, 1_000_000_000);

        let ctx = cli_context_default_for_tests();

        let mut output = Vec::new();
        let res = block_on(
            AcceptLinkCliCommand {
                code: link_pubkey.to_string(),
                side_z_interface: SIDE_Z_IFACE.to_string(),
                wait: false,
            }
            .execute(&ctx, &client, &mut output),
        );

        let err = res.unwrap_err().to_string();
        assert_eq!(
            err,
            "Interface 'Ethernet1/1' on side A device has bandwidth 500000000 which is less than link bandwidth 1000000000"
        );
    }

    #[test]
    fn test_cli_link_accept_rejects_insufficient_side_z_bandwidth() {
        // Side Z interface at 500 Mbps cannot accept a 1 Gbps link.
        let AcceptFixture {
            client,
            link_pubkey,
            ..
        } = make_accept_fixture(10_000_000_000, 500_000_000, 1_000_000_000);

        let ctx = cli_context_default_for_tests();

        let mut output = Vec::new();
        let res = block_on(
            AcceptLinkCliCommand {
                code: link_pubkey.to_string(),
                side_z_interface: SIDE_Z_IFACE.to_string(),
                wait: false,
            }
            .execute(&ctx, &client, &mut output),
        );

        let err = res.unwrap_err().to_string();
        assert_eq!(
            err,
            "Interface 'Ethernet1/2' on side Z device has bandwidth 500000000 which is less than link bandwidth 1000000000"
        );
    }
}
