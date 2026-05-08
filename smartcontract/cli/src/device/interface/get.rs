use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_program_common::validate_iface;
use doublezero_sdk::commands::{
    device::get::GetDeviceCommand, topology::list::ListTopologyCommand,
};
use serde::Serialize;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetDeviceInterfaceCliCommand {
    /// Device Pubkey or Code
    #[arg(value_parser = validate_pubkey_or_code, required = true)]
    pub device: String,
    /// Interface name
    #[arg(value_parser = validate_iface, required = true)]
    pub name: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct InterfaceDisplay {
    pub name: String,
    pub status: String,
    pub loopback_type: String,
    pub interface_cyoa: String,
    pub bandwidth: u64,
    pub cir: u64,
    pub mtu: u16,
    pub routing_mode: String,
    pub vlan_id: u16,
    pub ip_net: String,
    pub node_segment_idx: u16,
    pub user_tunnel_endpoint: bool,
    pub flex_algo_node_segments: String,
    pub device_pk: String,
}

impl GetDeviceInterfaceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (device_pk, device) = client.get_device(GetDeviceCommand {
            pubkey_or_code: self.device,
        })?;

        let interface = device
            .interfaces
            .iter()
            .find(|i| i.name.to_lowercase() == self.name.to_lowercase())
            .ok_or_else(|| eyre::eyre!("Interface '{}' not found", self.name))?;

        // Resolve flex-algo topology PDAs to names. Cache the topology map up
        // front so we don't run `list_topology` once per segment; on lookup
        // miss (e.g. the topology was deleted), fall back to a truncated pubkey.
        let flex_algo_node_segments = if interface.flex_algo_node_segments.is_empty() {
            String::new()
        } else {
            let topology_map = client
                .list_topology(ListTopologyCommand)
                .unwrap_or_default();
            interface
                .flex_algo_node_segments
                .iter()
                .map(|seg| {
                    let label = topology_map
                        .get(&seg.topology)
                        .map(|t| t.name.clone())
                        .unwrap_or_else(|| {
                            let s = seg.topology.to_string();
                            format!("{}…", &s[..8.min(s.len())])
                        });
                    format!("{}:{}", label, seg.node_segment_idx)
                })
                .collect::<Vec<_>>()
                .join("\n")
        };

        let display = InterfaceDisplay {
            name: interface.name.clone(),
            status: interface.status.to_string(),
            loopback_type: interface.loopback_type.to_string(),
            interface_cyoa: interface.interface_cyoa.to_string(),
            bandwidth: interface.bandwidth,
            cir: interface.cir,
            mtu: interface.mtu,
            routing_mode: interface.routing_mode.to_string(),
            vlan_id: interface.vlan_id,
            ip_net: interface.ip_net.to_string(),
            node_segment_idx: interface.node_segment_idx,
            user_tunnel_endpoint: interface.user_tunnel_endpoint,
            flex_algo_node_segments,
            device_pk: device_pk.to_string(),
        };

        if self.json {
            let json = serde_json::to_string_pretty(&display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = InterfaceDisplay::headers();
            let fields = display.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            let blank = String::new();
            for (header, value) in headers.iter().zip(fields.iter()) {
                let mut lines = value.split('\n');
                if let Some(first) = lines.next() {
                    writeln!(out, " {header:<max_len$} | {first}")?;
                }
                for cont in lines {
                    writeln!(out, " {blank:<max_len$} | {cont}")?;
                }
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        device::interface::get::GetDeviceInterfaceCliCommand, tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::device::get::GetDeviceCommand, AccountType, Device, DeviceStatus, DeviceType,
        Interface,
    };
    use doublezero_serviceability::state::{
        interface::{InterfaceStatus, InterfaceType, LoopbackType},
        topology::{FlexAlgoNodeSegment, TopologyConstraint, TopologyInfo},
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, str::FromStr};

    #[test]
    fn test_cli_device_interface_get() {
        let mut client = create_test_client();

        let device1_pubkey =
            Pubkey::from_str("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB").unwrap();
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::from_str_const(
                "1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR",
            ),
            owner: device1_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![Interface {
                status: InterfaceStatus::Activated,
                name: "eth0".to_string(),
                interface_type: InterfaceType::Physical,
                loopback_type: LoopbackType::None,
                interface_cyoa: doublezero_serviceability::state::interface::InterfaceCYOA::None,
                interface_dia: doublezero_serviceability::state::interface::InterfaceDIA::None,
                bandwidth: 1000,
                cir: 500,
                mtu: 9000,
                routing_mode: doublezero_serviceability::state::interface::RoutingMode::Static,
                vlan_id: 16,
                ip_net: "10.0.0.1/24".parse().unwrap(),
                node_segment_idx: 42,
                user_tunnel_endpoint: true,
                ..Default::default()
            }],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
            ..Default::default()
        };

        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device1_pubkey.to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((device1_pubkey, device1.clone())));
        client
            .expect_get_device()
            .returning(move |_| Err(eyre::eyre!("not found")));

        // Expected failure
        let mut output = Vec::new();
        let res = GetDeviceInterfaceCliCommand {
            device: Pubkey::new_unique().to_string(),
            name: "Eth0".to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success (table)
        let mut output = Vec::new();
        let res = GetDeviceInterfaceCliCommand {
            device: device1_pubkey.to_string(),
            name: "eth0".to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("name", "eth0"),
            "name row should contain interface name"
        );
        assert!(
            has_row("status", "activated"),
            "status row should contain value"
        );
        assert!(
            has_row("device_pk", &device1_pubkey.to_string()),
            "device_pk row should contain pubkey"
        );
    }

    #[test]
    fn test_cli_device_interface_get_displays_flex_algo_node_segments() {
        let mut client = create_test_client();

        let topo_a = Pubkey::new_unique();
        let topo_b = Pubkey::new_unique();
        let topo_unknown = Pubkey::new_unique();

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "dz1".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: device_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![Interface {
                status: InterfaceStatus::Activated,
                name: "Loopback256".to_string(),
                interface_type: InterfaceType::Loopback,
                loopback_type: LoopbackType::Vpnv4,
                interface_cyoa: doublezero_serviceability::state::interface::InterfaceCYOA::None,
                interface_dia: doublezero_serviceability::state::interface::InterfaceDIA::None,
                bandwidth: 0,
                cir: 0,
                mtu: 9000,
                routing_mode: doublezero_serviceability::state::interface::RoutingMode::Static,
                vlan_id: 0,
                ip_net: "10.99.0.1/32".parse().unwrap(),
                node_segment_idx: 11000,
                user_tunnel_endpoint: false,
                flex_algo_node_segments: vec![
                    FlexAlgoNodeSegment {
                        topology: topo_a,
                        node_segment_idx: 12001,
                    },
                    FlexAlgoNodeSegment {
                        topology: topo_b,
                        node_segment_idx: 12002,
                    },
                    FlexAlgoNodeSegment {
                        topology: topo_unknown,
                        node_segment_idx: 12003,
                    },
                ],
                ..Default::default()
            }],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
            ..Default::default()
        };

        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device_pubkey.to_string(),
            }))
            .returning(move |_| Ok((device_pubkey, device.clone())));

        // Only TOPO-A and TOPO-B are known; topo_unknown is missing from the
        // map so it should fall back to a truncated pubkey.
        client.expect_list_topology().returning(move |_| {
            let mut m = HashMap::new();
            m.insert(
                topo_a,
                TopologyInfo {
                    account_type: AccountType::Topology,
                    owner: Pubkey::new_unique(),
                    bump_seed: 1,
                    name: "TOPO-A".to_string(),
                    admin_group_bit: 0,
                    flex_algo_number: 128,
                    constraint: TopologyConstraint::IncludeAny,
                    reference_count: 0,
                },
            );
            m.insert(
                topo_b,
                TopologyInfo {
                    account_type: AccountType::Topology,
                    owner: Pubkey::new_unique(),
                    bump_seed: 1,
                    name: "TOPO-B".to_string(),
                    admin_group_bit: 1,
                    flex_algo_number: 129,
                    constraint: TopologyConstraint::IncludeAny,
                    reference_count: 0,
                },
            );
            Ok(m)
        });

        let mut output = Vec::new();
        let res = GetDeviceInterfaceCliCommand {
            device: device_pubkey.to_string(),
            name: "Loopback256".to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "{:?}", res.err());
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains("TOPO-A:12001"),
            "expected TOPO-A line; got:\n{output_str}"
        );
        assert!(
            output_str.contains("TOPO-B:12002"),
            "expected TOPO-B line; got:\n{output_str}"
        );
        let unknown_short = &topo_unknown.to_string()[..8];
        assert!(
            output_str.contains(&format!("{unknown_short}…:12003")),
            "expected fallback truncated-pubkey line; got:\n{output_str}"
        );
    }
}
