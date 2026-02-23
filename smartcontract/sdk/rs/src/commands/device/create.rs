use doublezero_program_common::{types::NetworkV4List, validate_account_code};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_device_pda,
    processors::device::create::DeviceCreateArgs,
    state::device::{DeviceDesiredStatus, DeviceType},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateDeviceCommand {
    pub code: String,
    pub contributor_pk: Pubkey,
    pub location_pk: Pubkey,
    pub exchange_pk: Pubkey,
    pub device_type: DeviceType,
    pub public_ip: Ipv4Addr,
    pub dz_prefixes: NetworkV4List,
    pub metrics_publisher: Pubkey,
    pub mgmt_vrf: String,
    pub desired_status: Option<DeviceDesiredStatus>,
}

impl CreateDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let mut code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;
        code.make_ascii_lowercase();

        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_device_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                    code,
                    device_type: self.device_type,
                    public_ip: self.public_ip,
                    dz_prefixes: self.dz_prefixes.clone(),
                    metrics_publisher_pk: self.metrics_publisher,
                    mgmt_vrf: self.mgmt_vrf.clone(),
                    desired_status: self.desired_status,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.contributor_pk, false),
                    AccountMeta::new(self.location_pk, false),
                    AccountMeta::new(self.exchange_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::device::create::CreateDeviceCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_device_pda, get_globalstate_pda},
        processors::device::create::DeviceCreateArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::DeviceType,
            exchange::{Exchange, ExchangeStatus},
            location::{Location, LocationStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_device_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        let location_pubkey = Pubkey::new_unique();
        let location = Location {
            account_type: AccountType::Location,
            owner: client.get_payer(),
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            name: "Test Location".to_string(),
            country: "UA".to_string(),
            code: "TEST".to_string(),
            lat: 50.4501,
            lng: 30.5234,
            loc_id: 1,
            status: LocationStatus::Activated,
        };

        client
            .expect_get()
            .with(predicate::eq(location_pubkey))
            .returning(move |_| Ok(AccountData::Location(location.clone())));

        let exchange_pubkey = Pubkey::new_unique();
        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: client.get_payer(),
            index: 2,
            bump_seed: 255,
            reference_count: 0,
            name: "Test Location".to_string(),
            code: "TEST".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 50.4501,
            lng: 30.5234,
            bgp_community: 1,
            unused: 0,
            status: ExchangeStatus::Activated,
        };
        client
            .expect_get()
            .with(predicate::eq(exchange_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        let contributor_pubkey = Pubkey::default();
        let (device_pubkey, _) = get_device_pda(&client.get_program_id(), 1);

        let pubmetrics_publisher = Pubkey::default();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                    code: "test_device".to_string(),
                    device_type: DeviceType::Hybrid,
                    public_ip: [10, 0, 0, 1].into(),
                    dz_prefixes: "10.0.0.0/8".parse().unwrap(),
                    metrics_publisher_pk: pubmetrics_publisher,
                    mgmt_vrf: "mgmt".to_string(),
                    desired_status: None,
                })),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(contributor_pubkey, false),
                    AccountMeta::new(location_pubkey, false),
                    AccountMeta::new(exchange_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let command = CreateDeviceCommand {
            code: "test_device".to_string(),
            contributor_pk: contributor_pubkey,
            location_pk: location_pubkey,
            exchange_pk: exchange_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.0/8".parse().unwrap(),
            metrics_publisher: pubmetrics_publisher,
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
        };

        let invalid_command = CreateDeviceCommand {
            code: "test/device".to_string(),
            ..command.clone()
        };

        let res = invalid_command.execute(&client);
        assert!(res.is_err());

        let res = command.execute(&client);
        assert!(res.is_ok());
    }
}
