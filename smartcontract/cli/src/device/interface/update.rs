use crate::{
    device::interface::types,
    doublezerocommand::CliCommand,
    poll_for_activation::poll_for_device_interface_activated,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_program_common::validate_iface;
use doublezero_sdk::commands::device::{
    get::GetDeviceCommand, interface::update::UpdateDeviceInterfaceCommand,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateDeviceInterfaceCliCommand {
    /// Device Pubkey or Code
    #[arg(value_parser = validate_pubkey_or_code, required = true)]
    pub pubkey_or_code: String,
    /// Interface name
    #[arg(value_parser = validate_iface, required = true)]
    pub name: String,
    /// Loopback type (if applicable)
    #[arg(long)]
    pub loopback_type: Option<types::LoopbackType>,
    /// VLAN ID (default: 0, i.e. not set)
    #[arg(long)]
    pub vlan_id: Option<u16>,
    /// Can terminate a user tunnel?
    #[arg(long)]
    pub user_tunnel_endpoint: Option<bool>,
    /// Wait for the device interface to be activated
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl UpdateDeviceInterfaceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (device_pk, device) = client
            .get_device(GetDeviceCommand {
                pubkey_or_code: self.pubkey_or_code.clone(),
            })
            .map_err(|_| {
                eyre::eyre!(
                    "Device with pubkey/code '{}' not found",
                    self.pubkey_or_code
                )
            })?;

        let (_, mut interface) = device
            .find_interface(&self.name)
            .map_err(|e| eyre::eyre!(e.to_string()))?;

        if let Some(loopback_type) = self.loopback_type.clone() {
            interface.loopback_type = loopback_type.into();
        }

        if interface.interface_type
            == doublezero_serviceability::state::device::InterfaceType::Loopback
            && interface.loopback_type
                == doublezero_serviceability::state::device::LoopbackType::None
        {
            return Err(eyre::eyre!(
                "Loopback type must be specified for Loopback interface type"
            ));
        }

        if interface.interface_type
            == doublezero_serviceability::state::device::InterfaceType::Physical
            && interface.loopback_type
                != doublezero_serviceability::state::device::LoopbackType::None
        {
            return Err(eyre::eyre!(
                "Loopback type must be None for Physical interface type"
            ));
        }

        let signature = client.update_device_interface(UpdateDeviceInterfaceCommand {
            pubkey: device_pk,
            name: self.name.clone(),
            loopback_type: self.loopback_type.map(|lt| lt.into()),
            vlan_id: self.vlan_id,
            user_tunnel_endpoint: self.user_tunnel_endpoint,
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
    use doublezero_sdk::{AccountType, CurrentInterfaceVersion, Device, DeviceStatus, DeviceType};
    use doublezero_serviceability::state::device::{
        Interface, InterfaceStatus, InterfaceType, LoopbackType,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_device_interface_update_success() {
        let mut client = create_test_client();

        let signature = Signature::new_unique();

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
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "Ethernet0".to_string(),
                    interface_type: InterfaceType::Physical,
                    loopback_type: LoopbackType::None,
                    vlan_id: 0,
                    ip_net: "10.0.0.1/24".parse().unwrap(),
                    node_segment_idx: 0,
                    user_tunnel_endpoint: true,
                }),
                Interface::V1(CurrentInterfaceVersion {
                    status: InterfaceStatus::Activated,
                    name: "Loopback0".to_string(),
                    interface_type: InterfaceType::Loopback,
                    loopback_type: LoopbackType::Vpnv4,
                    vlan_id: 16,
                    ip_net: "10.0.1.1/24".parse().unwrap(),
                    node_segment_idx: 0,
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
                pubkey_or_code: device1_pubkey.to_string(),
            }))
            .returning(move |_| Ok((device1_pubkey, device1.clone())));

        client
            .expect_update_device_interface()
            .with(predicate::eq(UpdateDeviceInterfaceCommand {
                pubkey: device1_pubkey,
                name: "Loopback0".to_string(),
                loopback_type: Some(LoopbackType::Ipv4),
                vlan_id: Some(20),
                user_tunnel_endpoint: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        // Expected success
        let mut output = Vec::new();
        let res = UpdateDeviceInterfaceCliCommand {
            pubkey_or_code: device1_pubkey.to_string(),
            name: "Loopback0".to_string(),
            loopback_type: Some(types::LoopbackType::Ipv4),
            vlan_id: Some(20),
            user_tunnel_endpoint: None,
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "{}", res.err().unwrap());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, format!("Signature: {signature}\n"));
    }
}
