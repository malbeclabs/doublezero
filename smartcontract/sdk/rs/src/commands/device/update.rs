use crate::{
    commands::{device::get::GetDeviceCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::device::update::DeviceUpdateArgs,
    state::device::{DeviceType, Interface},
    types::NetworkV4List,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateDeviceCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub device_type: Option<DeviceType>,
    pub public_ip: Option<Ipv4Addr>,
    pub dz_prefixes: Option<NetworkV4List>,
    pub metrics_publisher: Option<Pubkey>,
    pub contributor_pk: Option<Pubkey>,
    pub mgmt_vrf: Option<String>,
    pub interfaces: Option<Vec<Interface>>,
}

impl UpdateDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let code = self
            .code
            .as_ref()
            .map(|code| validate_account_code(code))
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device not found"))?;

        client.execute_transaction(
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                code,
                contributor_pk: self.contributor_pk,
                device_type: self.device_type,
                public_ip: self.public_ip,
                dz_prefixes: self.dz_prefixes.clone(),
                metrics_publisher_pk: self.metrics_publisher,
                mgmt_vrf: self.mgmt_vrf.clone(),
                interfaces: self.interfaces.clone(),
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(device.contributor_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::device::update::UpdateDeviceCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::get_contributor_pda,
        processors::device::update::DeviceUpdateArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::{Device, DeviceStatus, DeviceType},
        },
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_device_update_command() {
        let mut client = create_test_client();

        let (pda_pubkey, _) = get_contributor_pda(&client.get_program_id(), 1);

        let device_pubkey = Pubkey::new_unique();
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test_dev".to_string(),
            contributor_pk: Pubkey::default(),
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: pda_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
        };

        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device.clone())));
        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                    code: Some("test_device".to_string()),
                    device_type: Some(DeviceType::Switch),
                    public_ip: None,
                    dz_prefixes: Some("10.0.0.0/8".parse().unwrap()),
                    metrics_publisher_pk: None,
                    mgmt_vrf: Some("mgmt".to_string()),
                    interfaces: None,
                    contributor_pk: None,
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let update_command = UpdateDeviceCommand {
            pubkey: device_pubkey,
            code: Some("test_device".to_string()),
            contributor_pk: None,
            device_type: Some(DeviceType::Switch),
            public_ip: None,
            dz_prefixes: Some("10.0.0.0/8".parse().unwrap()),
            metrics_publisher: None,
            mgmt_vrf: Some("mgmt".to_string()),
            interfaces: None,
        };

        let update_invalid = UpdateDeviceCommand {
            code: Some("test/device".to_string()),
            ..update_command.clone()
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());

        let res = update_invalid.execute(&client);
        assert!(res.is_err());
    }
}
