use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_program_common::validate_iface;
use doublezero_sdk::{
    commands::device::{get::GetDeviceCommand, interface::delete::DeleteDeviceInterfaceCommand},
    InterfaceStatus, InterfaceType,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteDeviceInterfaceCliCommand {
    /// Device Pubkey or Code
    #[arg(value_parser = validate_pubkey_or_code, required = true)]
    pub device: String,
    /// Interface name
    #[arg(value_parser = validate_iface, required = true)]
    pub name: String,
}

impl DeleteDeviceInterfaceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, device) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: self.device,
            })
            .map_err(|_| eyre::eyre!("Device not found"))?;

        let (_, iface) = device
            .find_interface(&self.name)
            .map_err(|err| eyre::eyre!(err))?;

        // if a physical interface is Activated, it's part of a link and shouldn't be deleted.
        if iface.interface_type == InterfaceType::Physical
            && iface.status == InterfaceStatus::Activated
        {
            return Err(eyre::eyre!(
                "Cannot delete physical interface '{}' with status {}",
                iface.name,
                iface.status
            ));
        }

        let signature = client.delete_device_interface(DeleteDeviceInterfaceCommand {
            pubkey,
            name: self.name,
        })?;

        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_program_common::types::NetworkV4List;
    use doublezero_sdk::{AccountType, CurrentInterfaceVersion, Device, DeviceStatus};
    use doublezero_serviceability::state::interface::{
        InterfaceCYOA, InterfaceStatus, InterfaceType, LoopbackType, RoutingMode,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_device_interface_delete() {
        let mut client = create_test_client();

        let signature = Signature::new_unique();

        let device_pk = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: doublezero_sdk::DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: NetworkV4List::default(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![
                CurrentInterfaceVersion {
                    status: InterfaceStatus::Unlinked,
                    name: "Ethernet0".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    interface_cyoa: InterfaceCYOA::None,
                    interface_dia: doublezero_serviceability::state::interface::InterfaceDIA::None,
                    bandwidth: 1000,
                    cir: 500,
                    mtu: 1500,
                    routing_mode: RoutingMode::Static,
                    vlan_id: 0,
                    ip_net: "10.0.0.1/24".parse().unwrap(),
                    node_segment_idx: 12,
                    user_tunnel_endpoint: true,
                }
                .to_interface(),
                CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "Loopback0".to_string(),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::Vpnv4,
                    interface_cyoa: InterfaceCYOA::None,
                    interface_dia: doublezero_serviceability::state::interface::InterfaceDIA::None,
                    bandwidth: 500,
                    cir: 250,
                    mtu: 1400,
                    routing_mode: RoutingMode::Static,
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
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device_pk.to_string(),
            }))
            .returning(move |_| Ok((device_pk, device.clone())));

        client
            .expect_delete_device_interface()
            .with(predicate::eq(DeleteDeviceInterfaceCommand {
                pubkey: device_pk,
                name: "Ethernet0".to_string(),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = DeleteDeviceInterfaceCliCommand {
            device: device_pk.to_string(),
            name: "Ethernet0".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "Error: {}", res.unwrap_err());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, format!("Signature: {signature}\n"));
    }
}
