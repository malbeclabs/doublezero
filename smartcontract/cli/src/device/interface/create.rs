use crate::{
    device::interface::types::{InterfaceType, LoopbackType},
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::device::{get::GetDeviceCommand, update::UpdateDeviceCommand};
use doublezero_serviceability::{state::device, types::NetworkV4};
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateDeviceInterfaceCliCommand {
    /// Device Pubkey or Code
    #[arg(value_parser = validate_pubkey_or_code, required = true)]
    pub device: String,
    /// Interface name
    #[arg(required = true)]
    pub name: String,
    /// Interface type
    #[arg()]
    pub interface_type: InterfaceType,
    /// Loopback type (if applicable)
    #[arg(long, default_value = "none")]
    pub loopback_type: LoopbackType,
    /// VLAN ID (default: 0, i.e. not set)
    #[arg(long, default_value = "0")]
    pub vlan_id: u16,
    /// Can terminate a user tunnel?
    #[arg(long, default_value = "false")]
    pub user_tunnel_endpoint: bool,
}

impl CreateDeviceInterfaceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (device_pk, device) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: self.device.clone(),
            })
            .map_err(|_| eyre::eyre!("Device with pubkey/code '{}' not found", self.device))?;

        device
            .interfaces
            .iter()
            .find(|i| i.name == self.name)
            .map_or(Ok(()), |_| {
                Err(eyre::eyre!(
                    "Interface with name '{}' already exists",
                    self.name
                ))
            })?;

        if self.interface_type == InterfaceType::Loopback
            && self.loopback_type == LoopbackType::None
        {
            return Err(eyre::eyre!(
                "Loopback type must be specified for Loopback interface type"
            ));
        }

        if self.interface_type == InterfaceType::Physical
            && self.loopback_type != LoopbackType::None
        {
            return Err(eyre::eyre!(
                "Loopback type must be None for Physical interface type"
            ));
        }

        let mut interfaces = device.interfaces;
        interfaces.push(device::Interface {
            version: device::CURRENT_INTERFACE_VERSION,
            name: self.name.clone(),
            interface_type: self.interface_type.into(),
            loopback_type: self.loopback_type.into(),
            vlan_id: self.vlan_id,
            ip_net: NetworkV4::default(),
            node_segment_idx: 0,
            user_tunnel_endpoint: self.user_tunnel_endpoint,
        });
        interfaces.sort_by(|a, b| a.name.cmp(&b.name));

        let signature = client.update_device(UpdateDeviceCommand {
            pubkey: device_pk,
            code: None,
            device_type: None,
            public_ip: None,
            dz_prefixes: None,
            metrics_publisher: None,
            contributor_pk: None,
            bgp_asn: None,
            dia_bgp_asn: None,
            mgmt_vrf: None,
            dns_servers: None,
            ntp_servers: None,
            interfaces: Some(interfaces),
        })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        device::interface::create::CreateDeviceInterfaceCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::device::{get::GetDeviceCommand, update::UpdateDeviceCommand},
        AccountType, Device, DeviceStatus, DeviceType,
    };
    use doublezero_serviceability::{
        state::device::{Interface, InterfaceType, LoopbackType},
        types::NetworkV4,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_device_interface_create() {
        let mut client = create_test_client();

        let signature = Signature::new_unique();

        let device1_pubkey = Pubkey::new_unique();
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::from_str_const(
                "1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR",
            ),
            owner: device1_pubkey,
            bgp_asn: 42,
            dia_bgp_asn: 4242,
            mgmt_vrf: "default".to_string(),
            dns_servers: vec![[8, 8, 8, 8].into(), [8, 8, 4, 4].into()],
            ntp_servers: vec![[192, 168, 1, 1].into(), [192, 168, 1, 2].into()],
            interfaces: vec![Interface {
                version: super::device::CURRENT_INTERFACE_VERSION,
                name: "eth0".to_string(),
                interface_type: InterfaceType::Physical,
                loopback_type: LoopbackType::None,
                vlan_id: 16,
                ip_net: "10.0.0.1/24".parse().unwrap(),
                node_segment_idx: 0,
                user_tunnel_endpoint: true,
            }],
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device1_pubkey.to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((device1_pubkey, device1.clone())));
        client
            .expect_update_device()
            .with(predicate::eq(UpdateDeviceCommand {
                pubkey: device1_pubkey,
                code: None,
                device_type: None,
                public_ip: None,
                dz_prefixes: None,
                metrics_publisher: None,
                contributor_pk: None,
                bgp_asn: None,
                dia_bgp_asn: None,
                mgmt_vrf: None,
                dns_servers: None,
                ntp_servers: None,
                interfaces: Some(vec![
                    Interface {
                        version: super::device::CURRENT_INTERFACE_VERSION,
                        name: "eth0".to_string(),
                        interface_type: InterfaceType::Physical,
                        loopback_type: LoopbackType::None,
                        vlan_id: 16,
                        ip_net: "10.0.0.1/24".parse().unwrap(),
                        node_segment_idx: 0,
                        user_tunnel_endpoint: true,
                    },
                    Interface {
                        version: super::device::CURRENT_INTERFACE_VERSION,
                        name: "lo0".to_string(),
                        interface_type: InterfaceType::Loopback,
                        loopback_type: LoopbackType::Ipv4,
                        vlan_id: 20,
                        ip_net: NetworkV4::default(),
                        node_segment_idx: 0,
                        user_tunnel_endpoint: false,
                    },
                ]),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = CreateDeviceInterfaceCliCommand {
            device: device1_pubkey.to_string(),
            name: "lo0".to_string(),
            interface_type: super::InterfaceType::Loopback,
            loopback_type: super::LoopbackType::Ipv4,
            vlan_id: 20,
            user_tunnel_endpoint: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, format!("Signature: {signature}\n"));
    }
}
