use crate::{
    doublezerocommand::CliCommand,
    poll_for_activation::poll_for_device_activated,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_iface, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_sdk::commands::device::{get::GetDeviceCommand, update::UpdateDeviceCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteDeviceInterfaceCliCommand {
    /// Device Pubkey or Code
    #[arg(value_parser = validate_pubkey_or_code, required = true)]
    pub device: String,
    /// Interface name
    #[arg(value_parser = validate_iface, required = true)]
    pub name: String,
    /// Wait for the device to be activated
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl DeleteDeviceInterfaceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, mut device) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: self.device,
            })
            .map_err(|_| eyre::eyre!("Device not found"))?;

        let len_before = device.interfaces.len();
        device
            .interfaces
            .retain(|i| i.into_current_version().name.to_lowercase() != self.name.to_lowercase());
        let len_after = device.interfaces.len();

        if len_before == len_after {
            return Err(eyre::eyre!("Interface '{}' not found", self.name));
        }

        let signature = client.update_device(UpdateDeviceCommand {
            pubkey,
            code: None,
            device_type: None,
            public_ip: None,
            dz_prefixes: None,
            metrics_publisher: None,
            contributor_pk: None,
            mgmt_vrf: None,
            interfaces: Some(device.interfaces),
            max_users: None,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        if self.wait {
            let device = poll_for_device_activated(client, &pubkey)?;
            writeln!(out, "Status: {0}", device.status)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        device::interface::delete::DeleteDeviceInterfaceCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_program_common::types::NetworkV4List;
    use doublezero_sdk::{
        commands::device::{get::GetDeviceCommand, update::UpdateDeviceCommand},
        AccountType, CurrentInterfaceVersion, Device, DeviceStatus,
    };
    use doublezero_serviceability::state::device::{
        Interface, InterfaceStatus, InterfaceType, LoopbackType,
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
            device_type: doublezero_sdk::DeviceType::Switch,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: NetworkV4List::default(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "eth0".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 0,
                    ip_net: "10.0.0.1/24".parse().unwrap(),
                    node_segment_idx: 12,
                    user_tunnel_endpoint: true,
                }),
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "lo0".to_string(),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::Vpnv4,
                    vlan_id: 16,
                    ip_net: "10.0.1.1/24".parse().unwrap(),
                    node_segment_idx: 13,
                    user_tunnel_endpoint: false,
                }),
            ],
            max_users: 255,
            users_count: 0,
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
            .expect_update_device()
            .with(predicate::eq(UpdateDeviceCommand {
                pubkey: device_pk,
                code: None,
                device_type: None,
                public_ip: None,
                dz_prefixes: None,
                metrics_publisher: None,
                contributor_pk: None,
                mgmt_vrf: None,
                interfaces: Some(vec![Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "lo0".to_string(),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::Vpnv4,
                    vlan_id: 16,
                    ip_net: "10.0.1.1/24".parse().unwrap(),
                    node_segment_idx: 13,
                    user_tunnel_endpoint: false,
                })]),
                max_users: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = DeleteDeviceInterfaceCliCommand {
            device: device_pk.to_string(),
            name: "Eth0".to_string(),
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, format!("Signature: {signature}\n"));
    }
}
