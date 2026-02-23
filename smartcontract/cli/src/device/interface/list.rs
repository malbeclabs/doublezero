use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{
    commands::device::{get::GetDeviceCommand, list::ListDeviceCommand},
    CurrentInterfaceVersion, InterfaceType,
};
use doublezero_serviceability::state::interface::{
    InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode,
};
use serde::Serialize;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListDeviceInterfaceCliCommand {
    /// Device Pubkey or Code (empty for all)
    #[arg(value_parser = validate_pubkey_or_code)]
    pub device: Option<String>,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct DeviceInterfaceDisplay {
    pub device: String,
    pub name: String,
    pub interface_type: InterfaceType,
    pub loopback_type: LoopbackType,
    pub interface_cyoa: InterfaceCYOA,
    pub interface_dia: InterfaceDIA,
    pub bandwidth: u64,
    pub cir: u64,
    pub mtu: u16,
    pub routing_mode: RoutingMode,
    pub vlan_id: u16,
    pub ip_net: NetworkV4,
    pub node_segment_idx: u16,
    pub user_tunnel_endpoint: bool,
    pub status: String,
}

impl ListDeviceInterfaceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let iface_displays: Vec<DeviceInterfaceDisplay> = if let Some(device) = self.device {
            let (_, device) = client
                .get_device(GetDeviceCommand {
                    pubkey_or_code: device,
                })
                .map_err(|_| eyre::eyre!("Device not found"))?;

            device
                .interfaces
                .iter()
                .map(|iface| build_display(&iface.into_current_version(), &device.code))
                .collect()
        } else {
            let devices = client.list_device(ListDeviceCommand {})?;

            devices
                .iter()
                .flat_map(|(_, device)| {
                    device
                        .interfaces
                        .iter()
                        .map(|iface| build_display(&iface.into_current_version(), &device.code))
                })
                .collect()
        };

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

fn build_display(iface: &CurrentInterfaceVersion, device_code: &str) -> DeviceInterfaceDisplay {
    DeviceInterfaceDisplay {
        device: device_code.to_string(),
        name: iface.name.clone(),
        interface_type: iface.interface_type,
        loopback_type: iface.loopback_type,
        interface_cyoa: iface.interface_cyoa,
        interface_dia: iface.interface_dia,
        bandwidth: iface.bandwidth,
        cir: iface.cir,
        mtu: iface.mtu,
        routing_mode: iface.routing_mode,
        vlan_id: iface.vlan_id,
        ip_net: iface.ip_net,
        node_segment_idx: iface.node_segment_idx,
        user_tunnel_endpoint: iface.user_tunnel_endpoint,
        status: iface.status.to_string(),
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        device::interface::list::ListDeviceInterfaceCliCommand, tests::utils::create_test_client,
    };

    use doublezero_sdk::{
        commands::device::get::GetDeviceCommand, AccountType, CurrentInterfaceVersion, Device,
        DeviceStatus, DeviceType,
    };
    use doublezero_serviceability::state::interface::{
        InterfaceStatus, InterfaceType, LoopbackType,
    };
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
            reference_count: 0,
            code: "device1_code".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![
                CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "eth0".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    interface_cyoa:
                        doublezero_serviceability::state::interface::InterfaceCYOA::None,
                    interface_dia: doublezero_serviceability::state::interface::InterfaceDIA::None,
                    bandwidth: 1000,
                    cir: 500,
                    mtu: 1500,
                    routing_mode: doublezero_serviceability::state::interface::RoutingMode::Static,
                    vlan_id: 0,
                    ip_net: "10.0.0.1/24".parse().unwrap(),
                    node_segment_idx: 12,
                    user_tunnel_endpoint: true,
                }
                .to_interface(),
                CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "lo0".to_string(),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::Vpnv4,
                    interface_cyoa:
                        doublezero_serviceability::state::interface::InterfaceCYOA::None,
                    interface_dia: doublezero_serviceability::state::interface::InterfaceDIA::None,
                    bandwidth: 100,
                    cir: 50,
                    mtu: 1400,
                    routing_mode: doublezero_serviceability::state::interface::RoutingMode::Static,
                    vlan_id: 16,
                    ip_net: "10.0.1.1/24".parse().unwrap(),
                    node_segment_idx: 13,
                    user_tunnel_endpoint: false,
                }
                .to_interface(),
            ],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_users_count: 0,
            max_unicast_users: 0,
            max_multicast_users: 0,
            reserved_seats: 0,
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
            device: Some(device1_pubkey.to_string()),
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " device       | name | interface_type | loopback_type | interface_cyoa | interface_dia | bandwidth | cir | mtu  | routing_mode | vlan_id | ip_net      | node_segment_idx | user_tunnel_endpoint | status    \n device1_code | eth0 | physical       | none          | none           | none          | 1000      | 500 | 1500 | static       | 0       | 10.0.0.1/24 | 12               | true                 | activated \n device1_code | lo0  | loopback       | vpnv4         | none           | none          | 100       | 50  | 1400 | static       | 16      | 10.0.1.1/24 | 13               | false                | activated \n");

        let mut output = Vec::new();
        let res = ListDeviceInterfaceCliCommand {
            device: Some(device1_pubkey.to_string()),
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"device\":\"device1_code\",\"name\":\"eth0\",\"interface_type\":\"Physical\",\"loopback_type\":\"None\",\"interface_cyoa\":\"None\",\"interface_dia\":\"None\",\"bandwidth\":1000,\"cir\":500,\"mtu\":1500,\"routing_mode\":\"Static\",\"vlan_id\":0,\"ip_net\":\"10.0.0.1/24\",\"node_segment_idx\":12,\"user_tunnel_endpoint\":true,\"status\":\"activated\"},{\"device\":\"device1_code\",\"name\":\"lo0\",\"interface_type\":\"Loopback\",\"loopback_type\":\"Vpnv4\",\"interface_cyoa\":\"None\",\"interface_dia\":\"None\",\"bandwidth\":100,\"cir\":50,\"mtu\":1400,\"routing_mode\":\"Static\",\"vlan_id\":16,\"ip_net\":\"10.0.1.1/24\",\"node_segment_idx\":13,\"user_tunnel_endpoint\":false,\"status\":\"activated\"}]\n");
    }
}
