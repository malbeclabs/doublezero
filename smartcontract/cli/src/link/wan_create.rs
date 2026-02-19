use crate::{
    doublezerocommand::CliCommand,
    link::wan_create::utils::parse_pubkey,
    poll_for_activation::poll_for_link_activated,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{
        validate_code, validate_parse_bandwidth, validate_parse_delay_ms, validate_parse_jitter_ms,
        validate_parse_mtu, validate_pubkey_or_code,
    },
};
use clap::Args;
use doublezero_program_common::validate_iface;
use doublezero_sdk::{
    commands::{
        contributor::get::GetContributorCommand,
        device::get::GetDeviceCommand,
        link::{create::CreateLinkCommand, get::GetLinkCommand},
    },
    *,
};
use doublezero_serviceability::state::{
    interface::{InterfaceCYOA, InterfaceDIA},
    link::LinkDesiredStatus,
};
use eyre::eyre;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateWANLinkCliCommand {
    /// Link code, must be unique.
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Contributor (pubkey or code) associated with the device
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub contributor: String,
    /// Update link desired status (e.g. activated, soft-drained, hard-drained)
    #[arg(long)]
    pub desired_status: Option<LinkDesiredStatus>,
    /// Device Pubkey or code for side A.
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub side_a: String,
    /// Device interface name for side A.
    #[arg(long, value_parser = validate_iface)]
    pub side_a_interface: String,
    /// Device Pubkey or code for side Z.
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub side_z: String,
    /// Device interface name for side Z.
    #[arg(long, value_parser = validate_iface)]
    pub side_z_interface: String,
    /// Bandwidth (required). Accepts values in Kbps, Mbps, or Gbps.
    #[arg(long, value_parser = validate_parse_bandwidth)]
    pub bandwidth: u64,
    /// MTU (Maximum Transmission Unit) in bytes.
    #[arg(long, value_parser = validate_parse_mtu)]
    pub mtu: u32,
    /// RTT (Round Trip Time) delay in milliseconds.
    #[arg(long, value_parser = validate_parse_delay_ms)]
    pub delay_ms: f64,
    /// Jitter in milliseconds.
    #[arg(long, value_parser = validate_parse_jitter_ms)]
    pub jitter_ms: f64,
    /// Wait for the device to be activated
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl CreateWANLinkCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let contributor_pk = match parse_pubkey(&self.contributor) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .get_contributor(GetContributorCommand {
                        pubkey_or_code: self.contributor.clone(),
                    })
                    .map_err(|_| eyre::eyre!("Contributor not found"))?;
                pubkey
            }
        };

        let (side_a_pk, side_a_dev) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: self.side_a.clone(),
            })
            .map_err(|_| eyre::eyre!("Device not found"))?;

        let (side_z_pk, side_z_dev) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: self.side_z.clone(),
            })
            .map_err(|_| eyre::eyre!("Device not found"))?;

        let side_a_iface = side_a_dev
            .interfaces
            .iter()
            .map(|i| i.into_current_version())
            .find(|i| i.name.to_lowercase() == self.side_a_interface.to_lowercase())
            .ok_or_else(|| {
                eyre!(
                    "Interface '{}' not found on side A device",
                    self.side_a_interface
                )
            })?;

        if side_a_iface.interface_type != InterfaceType::Physical {
            return Err(eyre!(
                "Interface '{}' on side A device must be a physical interface",
                self.side_a_interface
            ));
        }

        if side_a_iface.status != InterfaceStatus::Unlinked {
            return Err(eyre!(
                "Interface '{}' on side A device must be unlinked",
                self.side_a_interface
            ));
        }

        if side_a_iface.interface_cyoa != InterfaceCYOA::None
            || side_a_iface.interface_dia != InterfaceDIA::None
        {
            return Err(eyre!(
                "Interface '{}' on side A device has a CYOA or DIA assignment and cannot be used for links",
                self.side_a_interface
            ));
        }

        let side_z_iface = side_z_dev
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

        if side_z_iface.interface_cyoa != InterfaceCYOA::None
            || side_z_iface.interface_dia != InterfaceDIA::None
        {
            return Err(eyre!(
                "Interface '{}' on side Z device has a CYOA or DIA assignment and cannot be used for links",
                self.side_z_interface
            ));
        }

        if client
            .get_link(GetLinkCommand {
                pubkey_or_code: self.code.clone(),
            })
            .is_ok()
        {
            return Err(eyre!("Link with code '{}' already exists", self.code));
        }

        let (signature, pubkey) = client.create_link(CreateLinkCommand {
            code: self.code.clone(),
            contributor_pk,
            desired_status: self.desired_status,
            side_a_pk,
            side_z_pk,
            link_type: LinkLinkType::WAN,
            bandwidth: self.bandwidth,
            mtu: self.mtu,
            delay_ns: (self.delay_ms * 1000000.0) as u64,
            jitter_ns: (self.jitter_ms * 1000000.0) as u64,
            side_a_iface_name: self.side_a_interface.clone(),
            side_z_iface_name: Some(self.side_z_interface.clone()),
        })?;

        writeln!(out, "Signature: {signature}",)?;

        if self.wait {
            let link = poll_for_link_activated(client, &pubkey)?;
            writeln!(out, "Status: {0}", link.status)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        link::wan_create::CreateWANLinkCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_sdk::{
        commands::{
            device::get::GetDeviceCommand,
            link::{create::CreateLinkCommand, get::GetLinkCommand},
        },
        get_device_pda, AccountType, CurrentInterfaceVersion, Device, DeviceStatus, DeviceType,
        Link, LinkLinkType, LinkStatus,
    };
    use doublezero_serviceability::state::interface::{
        InterfaceCYOA, InterfaceStatus, InterfaceType, LoopbackType,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_wan_link_create() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_device_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let location1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkca");
        let device1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk,
            location_pk: location1_pk,
            exchange_pk: exchange1_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.1.0.0/16".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            status: DeviceStatus::Activated,
            owner: pda_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![CurrentInterfaceVersion {
                status: InterfaceStatus::Unlinked,
                name: "Ethernet1/1".to_string(),
                interface_type: InterfaceType::Physical,
                loopback_type: LoopbackType::None,
                vlan_id: 16,
                ip_net: "10.2.0.1/24".parse().unwrap(),
                node_segment_idx: 0,
                user_tunnel_endpoint: true,
                ..Default::default()
            }
            .to_interface()],
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
        let location2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkce");
        let device2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk,
            location_pk: location2_pk,
            exchange_pk: exchange2_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.1.0.0/16".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            status: DeviceStatus::Activated,
            owner: pda_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![CurrentInterfaceVersion {
                status: InterfaceStatus::Unlinked,
                name: "Ethernet1/2".to_string(),
                interface_type: InterfaceType::Physical,
                loopback_type: LoopbackType::None,
                vlan_id: 16,
                ip_net: "10.2.0.2/24".parse().unwrap(),
                node_segment_idx: 0,
                user_tunnel_endpoint: true,
                ..Default::default()
            }
            .to_interface()],
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
        let location3_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquCkcx");
        let exchange3_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquCkce");
        let device3_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquCkcf");
        let device3 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk,
            location_pk: location3_pk,
            exchange_pk: exchange3_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.1.0.0/16".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            status: DeviceStatus::Activated,
            owner: pda_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![CurrentInterfaceVersion {
                status: InterfaceStatus::Unlinked,
                name: "Ethernet1/3".to_string(),
                interface_type: InterfaceType::Physical,
                loopback_type: LoopbackType::None,
                vlan_id: 16,
                ip_net: "10.2.0.3/24".parse().unwrap(),
                node_segment_idx: 0,
                user_tunnel_endpoint: true,
                ..Default::default()
            }
            .to_interface()],
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
        let link = Link {
            account_type: AccountType::Link,
            owner: pda_pubkey,
            index: 1,
            bump_seed: 255,
            side_a_pk: device1_pk,
            side_z_pk: device2_pk,
            link_type: LinkLinkType::WAN,
            bandwidth: 1000000000,
            mtu: 1500,
            delay_ns: 10000000000,
            jitter_ns: 5000000000,
            delay_override_ns: 0,
            tunnel_id: 500,
            tunnel_net: NetworkV4::default(),
            status: LinkStatus::Activated,
            code: "test".to_string(),
            contributor_pk,
            side_a_iface_name: "Ethernet1/1".to_string(),
            side_z_iface_name: "Ethernet1/2".to_string(),
            link_health: doublezero_serviceability::state::link::LinkHealth::ReadyForService,
            desired_status: doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device1_pk.to_string(),
            }))
            .returning(move |_| Ok((device1_pk, device1.clone())));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device2_pk.to_string(),
            }))
            .returning(move |_| Ok((device2_pk, device2.clone())));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device3_pk.to_string(),
            }))
            .returning(move |_| Ok((device3_pk, device3.clone())));
        client
            .expect_get_link()
            .with(predicate::eq(GetLinkCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .times(1)
            .returning(move |_| Err(eyre::eyre!("Link not found")));
        client
            .expect_create_link()
            .with(predicate::eq(CreateLinkCommand {
                code: "test".to_string(),
                contributor_pk,
                desired_status: None,
                side_a_pk: device1_pk,
                side_z_pk: device2_pk,
                link_type: LinkLinkType::WAN,
                bandwidth: 1000000000,
                mtu: 1500,
                delay_ns: 10000000000,
                jitter_ns: 5000000000,
                side_a_iface_name: "Ethernet1/1".to_string(),
                side_z_iface_name: Some("Ethernet1/2".to_string()),
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = CreateWANLinkCliCommand {
            code: "test".to_string(),
            contributor: contributor_pk.to_string(),
            desired_status: None,
            side_a: device1_pk.to_string(),
            side_z: device2_pk.to_string(),
            bandwidth: 1000000000,
            mtu: 1500,
            delay_ms: 10000.0,
            jitter_ms: 5000.0,
            side_a_interface: "Ethernet1/1".to_string(),
            side_z_interface: "Ethernet1/2".to_string(),
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "Error: {}", res.unwrap_err());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );

        client
            .expect_get_link()
            .with(predicate::eq(GetLinkCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((Pubkey::default(), link.clone())));

        let mut output = Vec::new();
        let res = CreateWANLinkCliCommand {
            code: "test".to_string(),
            contributor: contributor_pk.to_string(),
            desired_status: None,
            side_a: device2_pk.to_string(),
            side_z: device3_pk.to_string(),
            bandwidth: 1000000000,
            mtu: 1500,
            delay_ms: 10000.0,
            jitter_ms: 5000.0,
            side_a_interface: "Ethernet1/2".to_string(),
            side_z_interface: "Ethernet1/3".to_string(),
            wait: false,
        }
        .execute(&client, &mut output);
        assert_eq!(
            res.unwrap_err().to_string(),
            "Link with code 'test' already exists"
        );
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "");
    }

    #[test]
    fn test_cli_wan_link_create_rejects_cyoa_interface() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_device_pda(&client.get_program_id(), 1);

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let location1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkca");
        let device1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk,
            location_pk: location1_pk,
            exchange_pk: exchange1_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.1.0.0/16".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            status: DeviceStatus::Activated,
            owner: pda_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![CurrentInterfaceVersion {
                status: InterfaceStatus::Unlinked,
                name: "Ethernet1/1".to_string(),
                interface_type: InterfaceType::Physical,
                loopback_type: LoopbackType::None,
                vlan_id: 16,
                ip_net: "10.2.0.1/24".parse().unwrap(),
                node_segment_idx: 0,
                user_tunnel_endpoint: true,
                interface_cyoa: InterfaceCYOA::GREOverDIA,
                ..Default::default()
            }
            .to_interface()],
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
        let location2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkce");
        let device2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk,
            location_pk: location2_pk,
            exchange_pk: exchange2_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.1.0.0/16".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            status: DeviceStatus::Activated,
            owner: pda_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![CurrentInterfaceVersion {
                status: InterfaceStatus::Unlinked,
                name: "Ethernet1/2".to_string(),
                interface_type: InterfaceType::Physical,
                loopback_type: LoopbackType::None,
                vlan_id: 16,
                ip_net: "10.2.0.2/24".parse().unwrap(),
                node_segment_idx: 0,
                user_tunnel_endpoint: true,
                ..Default::default()
            }
            .to_interface()],
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
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device1_pk.to_string(),
            }))
            .returning(move |_| Ok((device1_pk, device1.clone())));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device2_pk.to_string(),
            }))
            .returning(move |_| Ok((device2_pk, device2.clone())));

        let mut output = Vec::new();
        let res = CreateWANLinkCliCommand {
            code: "test".to_string(),
            contributor: contributor_pk.to_string(),
            desired_status: None,
            side_a: device1_pk.to_string(),
            side_z: device2_pk.to_string(),
            bandwidth: 1000000000,
            mtu: 1500,
            delay_ms: 10000.0,
            jitter_ms: 5000.0,
            side_a_interface: "Ethernet1/1".to_string(),
            side_z_interface: "Ethernet1/2".to_string(),
            wait: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("CYOA or DIA assignment"),);
    }
}
