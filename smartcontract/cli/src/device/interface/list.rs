use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use doublezero_serviceability::{
    state::device::{InterfaceType, LoopbackType},
    types::NetworkV4,
};
use serde::Serialize;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListDeviceInterfaceCliCommand {
    /// Device Pubkey or Code
    #[arg(long, value_parser = validate_pubkey_or_code, required = true)]
    pub pubkey_or_code: String,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct DeviceInterfaceDisplay {
    pub name: String,
    pub interface_type: InterfaceType,
    pub loopback_type: LoopbackType,
    pub vlan_id: u16,
    pub ip_net: NetworkV4,
    pub node_segment_idx: u16,
    pub user_tunnel_endpoint: bool,
}

impl ListDeviceInterfaceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, device) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: self.pubkey_or_code.clone(),
            })
            .map_err(|_| eyre::eyre!("Device not found"))?;

        let iface_displays: Vec<DeviceInterfaceDisplay> = device
            .interfaces
            .iter()
            .map(|iface| DeviceInterfaceDisplay {
                name: iface.name.clone(),
                interface_type: iface.interface_type,
                loopback_type: iface.loopback_type,
                vlan_id: iface.vlan_id,
                ip_net: iface.ip_net,
                node_segment_idx: iface.node_segment_idx,
                user_tunnel_endpoint: iface.user_tunnel_endpoint,
            })
            .collect();

        let res = if self.json {
            serde_json::to_string_pretty(&iface_displays)?
        } else if self.json_compact {
            serde_json::to_string(&iface_displays)?
        } else {
            Table::new(iface_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        device::interface::list::ListDeviceInterfaceCliCommand, tests::utils::create_test_client,
    };

    use doublezero_sdk::{
        commands::device::get::GetDeviceCommand, AccountType, Device, DeviceStatus, DeviceType,
    };
    use doublezero_serviceability::state::device::{Interface, InterfaceType, LoopbackType};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_device_interface_list() {
        let mut client = create_test_client();

        let device1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            code: "device1_code".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            bgp_asn: 0,
            dia_bgp_asn: 0,
            mgmt_vrf: "default".to_string(),
            dns_servers: vec![[8, 8, 8, 8].into(), [8, 8, 4, 4].into()],
            ntp_servers: vec![[192, 168, 1, 1].into(), [192, 168, 1, 2].into()],
            interfaces: vec![
                Interface {
                    name: "eth0".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 0,
                    ip_net: "10.0.0.1/24".parse().unwrap(),
                    node_segment_idx: 12,
                    user_tunnel_endpoint: true,
                },
                Interface {
                    name: "lo0".to_string(),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::Vpnv4,
                    vlan_id: 16,
                    ip_net: "10.0.1.1/24".parse().unwrap(),
                    node_segment_idx: 13,
                    user_tunnel_endpoint: false,
                },
            ],
        };

        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device1_pubkey.to_string(),
            }))
            .times(2)
            .returning(move |_| Ok((device1_pubkey, device1.clone())));
        client
            .expect_get_device()
            .returning(move |_| Err(eyre::eyre!("not found")));

        let mut output = Vec::new();
        let res = ListDeviceInterfaceCliCommand {
            pubkey_or_code: device1_pubkey.to_string(),
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " name | interface_type | loopback_type | vlan_id | ip_net      | node_segment_idx | user_tunnel_endpoint \n eth0 | physical       | none          | 0       | 10.0.0.1/24 | 12               | true                 \n lo0  | loopback       | vpnv4         | 16      | 10.0.1.1/24 | 13               | false                \n");

        let mut output = Vec::new();
        let res = ListDeviceInterfaceCliCommand {
            pubkey_or_code: device1_pubkey.to_string(),
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"name\":\"eth0\",\"interface_type\":\"Physical\",\"loopback_type\":\"None\",\"vlan_id\":0,\"ip_net\":\"10.0.0.1/24\",\"node_segment_idx\":12,\"user_tunnel_endpoint\":true},{\"name\":\"lo0\",\"interface_type\":\"Loopback\",\"loopback_type\":\"Vpnv4\",\"vlan_id\":16,\"ip_net\":\"10.0.1.1/24\",\"node_segment_idx\":13,\"user_tunnel_endpoint\":false}]\n");
    }
}
