use crate::{
    doublezerocommand::CliCommand,
    poll_for_activation::poll_for_link_activated,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
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
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, _) = client.get_link(GetLinkCommand {
            pubkey_or_code: self.code.clone(),
        })?;

        let (_, link) = client.get_link(GetLinkCommand {
            pubkey_or_code: pubkey.to_string(),
        })?;

        let (_, device_z) = client.get_device(GetDeviceCommand {
            pubkey_or_code: link.side_z_pk.to_string(),
        })?;

        let side_z_iface = device_z
            .interfaces
            .iter()
            .map(|i| i.into_current_version())
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
    use crate::{
        doublezerocommand::CliCommand,
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
        DeviceType, InterfaceStatus, Link, LinkLinkType, LinkStatus,
    };
    use doublezero_serviceability::state::interface::{
        CurrentInterfaceVersion, InterfaceType, LoopbackType,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_link_accept() {
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
        let (pda_pubkey, _bump_seed) = get_link_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let device1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb");
        let device1 = Device {
            account_type: AccountType::Device,
            owner: Pubkey::default(),
            bump_seed: 255,
            index: 1,
            reference_count: 0,
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            code: "dev01".to_string(),

            interfaces: vec![CurrentInterfaceVersion {
                name: "Ethernet1/1".to_string(),
                status: InterfaceStatus::Unlinked,
                interface_type: InterfaceType::Physical,
                ..Default::default()
            }
            .to_interface()],
            device_type: DeviceType::Hybrid,
            public_ip: "127.0.0.1".parse().unwrap(),
            status: DeviceStatus::Activated,
            dz_prefixes: "10.0.0.1/32".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            contributor_pk,
            mgmt_vrf: "default".to_string(),
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_users_count: 0,
            max_unicast_users: 0,
            max_multicast_users: 0,
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
            owner: Pubkey::default(),
            bump_seed: 255,
            index: 2,
            reference_count: 0,
            code: "dev02".to_string(),
            interfaces: vec![CurrentInterfaceVersion {
                status: InterfaceStatus::Unlinked,
                name: "Ethernet1/2".to_string(),
                interface_type: InterfaceType::Physical,
                loopback_type: LoopbackType::None,
                vlan_id: 0,
                ip_net: "10.0.0.1/32".parse().unwrap(),
                node_segment_idx: 0,
                user_tunnel_endpoint: false,
                ..Default::default()
            }
            .to_interface()],
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: doublezero_sdk::DeviceType::Hybrid,
            public_ip: "127.0.0.1".parse().unwrap(),
            status: DeviceStatus::Activated,
            dz_prefixes: "10.0.0.1/32".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            contributor_pk,
            mgmt_vrf: "default".to_string(),
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_users_count: 0,
            max_unicast_users: 0,
            max_multicast_users: 0,
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
            bandwidth: 1000000000,
            mtu: 1500,
            delay_ns: 10000000000,
            jitter_ns: 5000000000,
            delay_override_ns: 0,
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/16".parse().unwrap(),
            status: LinkStatus::Requested,
            owner: pda_pubkey,
            side_a_iface_name: "Ethernet1/1".to_string(),
            side_z_iface_name: "Ethernet1/2".to_string(),
            link_health: doublezero_serviceability::state::link::LinkHealth::ReadyForService,
            desired_status: doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
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
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, tunnel.clone())));
        client
            .expect_accept_link()
            .with(predicate::eq(AcceptLinkCommand {
                link_pubkey: pda_pubkey,
                side_z_iface_name: "Ethernet1/2".to_string(),
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = AcceptLinkCliCommand {
            code: pda_pubkey.to_string(),
            side_z_interface: "Ethernet1/2".to_string(),
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "Error: {}", res.unwrap_err());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
