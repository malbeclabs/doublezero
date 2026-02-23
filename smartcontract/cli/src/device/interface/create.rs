use crate::{
    device::interface::types,
    doublezerocommand::CliCommand,
    poll_for_activation::poll_for_device_interface_activated,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_parse_bandwidth, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_program_common::{types::network_v4::NetworkV4, validate_iface};
use doublezero_sdk::commands::device::{
    get::GetDeviceCommand, interface::create::CreateDeviceInterfaceCommand,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateDeviceInterfaceCliCommand {
    /// Device Pubkey or Code
    #[arg(value_parser = validate_pubkey_or_code, required = true)]
    pub device: String,
    /// Interface name
    #[arg(value_parser = validate_iface, required = true)]
    pub name: String,
    /// Loopback type (for loopback interfaces)
    #[arg(long)]
    pub loopback_type: Option<types::LoopbackType>,
    /// Interface CYOA (for CYOA interfaces)
    #[arg(long)]
    pub interface_cyoa: Option<types::InterfaceCYOA>,
    /// DIA Port (for DIA interfaces)
    #[arg(long)]
    pub interface_dia: Option<types::InterfaceDIA>,
    /// IP Network
    #[arg(long)]
    pub ip_net: Option<NetworkV4>,
    /// Bandwidth in Mbps
    #[arg(long, value_parser = validate_parse_bandwidth, default_value = "0")]
    pub bandwidth: u64,
    /// Committed Information Rate in Mbps
    #[arg(long, value_parser = validate_parse_bandwidth, default_value = "0")]
    pub cir: u64,
    /// MTU
    #[arg(long, default_value = "1500")]
    pub mtu: u16,
    /// Routing mode
    #[arg(long, default_value = "static")]
    pub routing_mode: types::RoutingMode,
    /// VLAN ID (default: 0, i.e. not set)
    #[arg(long, default_value = "0")]
    pub vlan_id: u16,
    /// Can terminate a user tunnel?
    #[arg(long)]
    pub user_tunnel_endpoint: Option<bool>,
    /// Wait for the device to be activated
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
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
            .find(|i| i.into_current_version().name == self.name)
            .map_or(Ok(()), |_| {
                Err(eyre::eyre!(
                    "Interface with name '{}' already exists",
                    self.name
                ))
            })?;

        let (signature, _) = client.create_device_interface(CreateDeviceInterfaceCommand {
            pubkey: device_pk,
            name: self.name.clone(),
            ip_net: self.ip_net,
            loopback_type: self.loopback_type.map(|lt| lt.into()).unwrap_or_default(),
            interface_cyoa: self.interface_cyoa.map(|ic| ic.into()).unwrap_or_default(),
            interface_dia: self.interface_dia.map(|id| id.into()).unwrap_or_default(),
            bandwidth: self.bandwidth,
            cir: self.cir,
            mtu: self.mtu,
            routing_mode: self.routing_mode.into(),
            vlan_id: self.vlan_id,
            user_tunnel_endpoint: self.user_tunnel_endpoint.unwrap_or(false),
        })?;
        writeln!(out, "Signature: {signature}")?;

        if self.wait {
            let interface = poll_for_device_interface_activated(client, &device_pk, &self.name)?;
            writeln!(out, "Status: {0}", interface.status)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_sdk::{
        AccountType, CurrentInterfaceVersion, Device, DeviceStatus, DeviceType, InterfaceStatus,
        InterfaceType, LoopbackType,
    };
    use doublezero_serviceability::state::interface::{InterfaceCYOA, InterfaceDIA, RoutingMode};
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
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::from_str_const(
                "1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR",
            ),
            owner: device1_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![CurrentInterfaceVersion {
                status: InterfaceStatus::Pending,
                name: "Ethernet0".to_string(),
                interface_type: InterfaceType::Physical,
                loopback_type: LoopbackType::None,
                interface_cyoa: InterfaceCYOA::None,
                interface_dia: InterfaceDIA::None,
                bandwidth: 1000,
                cir: 500,
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 16,
                ip_net: "10.0.0.1/24".parse().unwrap(),
                node_segment_idx: 0,
                user_tunnel_endpoint: true,
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
            reserved_seats: 0,
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
            .expect_create_device_interface()
            .with(predicate::eq(CreateDeviceInterfaceCommand {
                pubkey: device1_pubkey,
                name: "Loopback0".to_string(),
                loopback_type: LoopbackType::Ipv4,
                interface_cyoa: InterfaceCYOA::GREOverDIA,
                interface_dia: InterfaceDIA::DIA,
                ip_net: None,
                bandwidth: 0,
                cir: 0,
                mtu: 1500,
                routing_mode: RoutingMode::Static,
                vlan_id: 20,
                user_tunnel_endpoint: false,
            }))
            .times(1)
            .returning(move |_| Ok((signature, device1_pubkey)));

        let mut output = Vec::new();
        let res = CreateDeviceInterfaceCliCommand {
            device: device1_pubkey.to_string(),
            name: "Loopback0".to_string(),
            loopback_type: Some(types::LoopbackType::Ipv4),
            interface_cyoa: Some(types::InterfaceCYOA::GREOverDIA),
            interface_dia: Some(types::InterfaceDIA::DIA),
            ip_net: None,
            bandwidth: 0,
            cir: 0,
            mtu: 1500,
            routing_mode: types::RoutingMode::Static,
            vlan_id: 20,
            user_tunnel_endpoint: None,
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, format!("Signature: {signature}\n"));
    }
}
