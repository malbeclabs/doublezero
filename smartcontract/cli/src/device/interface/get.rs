use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_program_common::validate_iface;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetDeviceInterfaceCliCommand {
    /// Device Pubkey or Code
    #[arg(value_parser = validate_pubkey_or_code, required = true)]
    pub device: String,
    /// Interface name
    #[arg(value_parser = validate_iface, required = true)]
    pub name: String,
}

impl GetDeviceInterfaceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (device_pk, device) = client.get_device(GetDeviceCommand {
            pubkey_or_code: self.device,
        })?;

        let interface = device
            .interfaces
            .iter()
            .map(|i| i.into_current_version())
            .find(|i| i.name.to_lowercase() == self.name.to_lowercase())
            .ok_or_else(|| eyre::eyre!("Interface '{}' not found", self.name))?;

        writeln!(
            out,
            "name: {}\r\n\
status: {}\r\n\
loopback_type: {}\r\n\
interface_cyoa: {}\r\n\
bandwidth: {}\r\n\
cir: {}\r\n\
mtu: {}\r\n\
routing_mode: {}\r\n\
vlan_id: {}\r\n\
ip_net: {}\r\n\
node_segment_idx: {}\r\n\
user_tunnel_endpoint: {}\r\n\
device_pk: {}",
            interface.name,
            interface.status,
            interface.loopback_type,
            interface.interface_cyoa,
            interface.bandwidth,
            interface.cir,
            interface.mtu,
            interface.routing_mode,
            interface.vlan_id,
            interface.ip_net,
            interface.node_segment_idx,
            interface.user_tunnel_endpoint,
            device_pk,
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        device::interface::get::GetDeviceInterfaceCliCommand, tests::utils::create_test_client,
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
    use std::str::FromStr;

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
            interfaces: vec![CurrentInterfaceVersion {
                status: InterfaceStatus::Activated,
                name: "eth0".to_string(),
                interface_type: InterfaceType::Physical,
                loopback_type: LoopbackType::None,
                interface_cyoa: doublezero_serviceability::state::interface::InterfaceCYOA::None,
                interface_dia: doublezero_serviceability::state::interface::InterfaceDIA::None,
                bandwidth: 1000,
                cir: 500,
                mtu: 1500,
                routing_mode: doublezero_serviceability::state::interface::RoutingMode::Static,
                vlan_id: 16,
                ip_net: "10.0.0.1/24".parse().unwrap(),
                node_segment_idx: 42,
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
        /*****************************************************************************************************/
        // Expected failure
        let mut output = Vec::new();
        let res = GetDeviceInterfaceCliCommand {
            device: Pubkey::new_unique().to_string(),
            name: "Eth0".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success
        let mut output = Vec::new();
        let res = GetDeviceInterfaceCliCommand {
            device: device1_pubkey.to_string(),
            name: "eth0".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "name: eth0\r\nstatus: activated\r\nloopback_type: none\r\ninterface_cyoa: none\r\nbandwidth: 1000\r\ncir: 500\r\nmtu: 1500\r\nrouting_mode: static\r\nvlan_id: 16\r\nip_net: 10.0.0.1/24\r\nnode_segment_idx: 42\r\nuser_tunnel_endpoint: true\r\ndevice_pk: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\n");
    }
}
