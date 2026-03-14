use doublezero_program_common::{types::NetworkV4List, validate_account_code};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_device_pda, get_globalconfig_pda, get_resource_extension_pda},
    processors::device::create::DeviceCreateArgs,
    resource::ResourceType,
    state::{
        device::{DeviceDesiredStatus, DeviceType},
        feature_flags::{is_feature_enabled, FeatureFlag},
    },
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

        let use_onchain_allocation =
            is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation);

        let (pda_pubkey, _) =
            get_device_pda(&client.get_program_id(), globalstate.account_index + 1);

        let mut accounts = vec![
            AccountMeta::new(pda_pubkey, false),
            AccountMeta::new(self.contributor_pk, false),
            AccountMeta::new(self.location_pk, false),
            AccountMeta::new(self.exchange_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        let resource_count: u8 = if use_onchain_allocation {
            let (globalconfig_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
            accounts.push(AccountMeta::new(globalconfig_pubkey, false));

            // TunnelIds(device_pk, 0) + DzPrefixBlock(device_pk, 0..dz_prefixes.len())
            let tunnel_ids_pda = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::TunnelIds(pda_pubkey, 0),
            );
            accounts.push(AccountMeta::new(tunnel_ids_pda.0, false));

            for idx in 0..self.dz_prefixes.len() {
                let dz_prefix_pda = get_resource_extension_pda(
                    &client.get_program_id(),
                    ResourceType::DzPrefixBlock(pda_pubkey, idx),
                );
                accounts.push(AccountMeta::new(dz_prefix_pda.0, false));
            }

            (1 + self.dz_prefixes.len()) as u8
        } else {
            0
        };

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
                    resource_count,
                }),
                accounts,
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::device::create::CreateDeviceCommand, tests::utils::create_test_client,
        DoubleZeroClient, MockDoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{
            get_device_pda, get_globalconfig_pda, get_globalstate_pda, get_resource_extension_pda,
        },
        processors::device::create::DeviceCreateArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::DeviceType,
            exchange::{Exchange, ExchangeStatus},
            feature_flags::FeatureFlag,
            globalstate::GlobalState,
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
                    resource_count: 0,
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

        // Use mixed-case input to verify SDK lowercases device codes,
        // preventing duplicates like "Test_Device" vs "test_device"
        let command = CreateDeviceCommand {
            code: "Test_Device".to_string(),
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

    #[test]
    fn test_commands_device_create_with_onchain_allocation() {
        let mut client = MockDoubleZeroClient::new();

        let payer = Pubkey::new_unique();
        client.expect_get_payer().returning(move || payer);
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let (globalstate_pubkey, bump_seed) = get_globalstate_pda(&program_id);
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed,
            account_index: 0,
            foundation_allowlist: vec![],
            _device_allowlist: vec![],
            _user_allowlist: vec![],
            activator_authority_pk: Pubkey::new_unique(),
            sentinel_authority_pk: Pubkey::new_unique(),
            contributor_airdrop_lamports: 1_000_000_000,
            user_airdrop_lamports: 40_000,
            health_oracle_pk: Pubkey::new_unique(),
            qa_allowlist: vec![],
            feature_flags: FeatureFlag::OnChainAllocation.to_mask(),
            feed_authority_pk: Pubkey::default(),
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let contributor_pubkey = Pubkey::new_unique();
        let location_pubkey = Pubkey::new_unique();
        let exchange_pubkey = Pubkey::new_unique();
        let (device_pubkey, _) = get_device_pda(&program_id, 1);
        let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
        let (tunnel_ids_pda, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
        let (dz_prefix0_pda, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

        let metrics_publisher = Pubkey::default();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                    code: "test_device".to_string(),
                    device_type: DeviceType::Hybrid,
                    public_ip: [10, 0, 0, 1].into(),
                    dz_prefixes: "10.0.0.0/8".parse().unwrap(),
                    metrics_publisher_pk: metrics_publisher,
                    mgmt_vrf: "mgmt".to_string(),
                    desired_status: None,
                    resource_count: 2,
                })),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(contributor_pubkey, false),
                    AccountMeta::new(location_pubkey, false),
                    AccountMeta::new(exchange_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(globalconfig_pubkey, false),
                    AccountMeta::new(tunnel_ids_pda, false),
                    AccountMeta::new(dz_prefix0_pda, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateDeviceCommand {
            code: "Test_Device".to_string(),
            contributor_pk: contributor_pubkey,
            location_pk: location_pubkey,
            exchange_pk: exchange_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.0/8".parse().unwrap(),
            metrics_publisher,
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
