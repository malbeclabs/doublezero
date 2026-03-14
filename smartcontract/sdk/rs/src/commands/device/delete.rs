use crate::{
    commands::{
        device::get::GetDeviceCommand, globalstate::get::GetGlobalStateCommand,
        resource::get::GetResourceCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::device::delete::DeviceDeleteArgs,
    resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteDeviceCommand {
    pub pubkey: Pubkey,
}

impl DeleteDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device not found"))?;

        // Try to discover resource accounts for atomic close
        let mut resource_accounts = vec![];
        let mut owner_accounts = vec![];
        for idx in 0..device.dz_prefixes.len() + 1 {
            let resource_type = match idx {
                0 => ResourceType::TunnelIds(self.pubkey, 0),
                _ => ResourceType::DzPrefixBlock(self.pubkey, idx - 1),
            };
            match (GetResourceCommand { resource_type }).execute(client) {
                Ok((pda, resource)) => {
                    resource_accounts.push(AccountMeta::new(pda, false));
                    owner_accounts.push(AccountMeta::new(resource.owner, false));
                }
                Err(_) => {
                    // Resources don't exist (device never activated) → legacy path
                    resource_accounts.clear();
                    owner_accounts.clear();
                    break;
                }
            }
        }

        if resource_accounts.is_empty() {
            // Legacy path
            client.execute_transaction(
                DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs::default()),
                vec![
                    AccountMeta::new(self.pubkey, false),
                    AccountMeta::new(device.contributor_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
        } else {
            // Atomic path
            let resource_count = resource_accounts.len() as u8;
            let mut accounts = vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(device.contributor_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(device.location_pk, false),
                AccountMeta::new(device.exchange_pk, false),
            ];
            accounts.extend(resource_accounts);
            accounts.extend(owner_accounts);
            accounts.push(AccountMeta::new(device.owner, false));

            client.execute_transaction(
                DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs { resource_count }),
                accounts,
            )
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{tests::utils::create_test_client, MockDoubleZeroClient};
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        id_allocator::IdAllocator,
        pda::{get_globalstate_pda, get_resource_extension_pda},
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            device::*,
            globalstate::GlobalState,
            resource_extension::{Allocator, ResourceExtensionOwned},
        },
    };
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    fn make_test_device(
        owner: Pubkey,
        contributor_pk: Pubkey,
        location_pk: Pubkey,
        exchange_pk: Pubkey,
    ) -> Device {
        Device {
            account_type: AccountType::Device,
            owner,
            index: 1,
            bump_seed: 0,
            code: "dev1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: vec!["100.1.0.0/23".parse::<NetworkV4>().unwrap()].into(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            contributor_pk,
            location_pk,
            exchange_pk,
            max_users: 128,
            users_count: 0,
            reference_count: 0,
            status: DeviceStatus::Drained,
            desired_status: DeviceDesiredStatus::Drained,
            device_health: DeviceHealth::Unknown,
            interfaces: vec![],
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        }
    }

    #[test]
    fn test_commands_device_delete_legacy() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let device_pubkey = Pubkey::new_unique();
        let contributor_pk = Pubkey::new_unique();
        let location_pk = Pubkey::new_unique();
        let exchange_pk = Pubkey::new_unique();

        // Device with no resources (pending, never activated)
        let mut device =
            make_test_device(client.get_payer(), contributor_pk, location_pk, exchange_pk);
        device.status = DeviceStatus::Pending;

        let device_clone = device.clone();
        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device_clone.clone())));

        // TunnelIds resource lookup should fail (not activated)
        let (tunnel_ids_pda, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            ResourceType::TunnelIds(device_pubkey, 0),
        );
        client
            .expect_get()
            .with(predicate::eq(tunnel_ids_pda))
            .returning(|_| Err(eyre::eyre!("not found")));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteDevice(
                    DeviceDeleteArgs::default(),
                )),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteDeviceCommand {
            pubkey: device_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_device_delete_atomic() {
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
            feature_flags: 0,
            feed_authority_pk: Pubkey::default(),
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let device_pubkey = Pubkey::new_unique();
        let contributor_pk = Pubkey::new_unique();
        let location_pk = Pubkey::new_unique();
        let exchange_pk = Pubkey::new_unique();
        let device = make_test_device(payer, contributor_pk, location_pk, exchange_pk);

        let device_clone = device.clone();
        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device_clone.clone())));

        // Resource accounts exist (device was activated)
        let res_owner = Pubkey::new_unique();
        let (tunnel_ids_pda, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
        let res_owner_clone = res_owner;
        client
            .expect_get()
            .with(predicate::eq(tunnel_ids_pda))
            .returning(move |_| {
                Ok(AccountData::ResourceExtension(ResourceExtensionOwned {
                    account_type: AccountType::ResourceExtension,
                    owner: res_owner_clone,
                    bump_seed: 0,
                    associated_with: Pubkey::default(),
                    allocator: Allocator::Id(IdAllocator::new((0, 1)).unwrap()),
                    storage: vec![],
                }))
            });

        let (dz_prefix_pda, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));
        let res_owner_clone2 = res_owner;
        client
            .expect_get()
            .with(predicate::eq(dz_prefix_pda))
            .returning(move |_| {
                Ok(AccountData::ResourceExtension(ResourceExtensionOwned {
                    account_type: AccountType::ResourceExtension,
                    owner: res_owner_clone2,
                    bump_seed: 0,
                    associated_with: Pubkey::default(),
                    allocator: Allocator::Id(IdAllocator::new((0, 1)).unwrap()),
                    storage: vec![],
                }))
            });

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs {
                    resource_count: 2,
                })),
                predicate::eq(vec![
                    AccountMeta::new(device_pubkey, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(location_pk, false),
                    AccountMeta::new(exchange_pk, false),
                    AccountMeta::new(tunnel_ids_pda, false),
                    AccountMeta::new(dz_prefix_pda, false),
                    AccountMeta::new(res_owner, false),
                    AccountMeta::new(res_owner, false),
                    AccountMeta::new(payer, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteDeviceCommand {
            pubkey: device_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
