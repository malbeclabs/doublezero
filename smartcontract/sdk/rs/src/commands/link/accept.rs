use crate::{
    commands::{
        device::get::GetDeviceCommand, globalstate::get::GetGlobalStateCommand,
        link::get::GetLinkCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_resource_extension_pda,
    processors::link::accept::LinkAcceptArgs,
    resource::ResourceType,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        link::LinkStatus,
    },
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct AcceptLinkCommand {
    pub link_pubkey: Pubkey,
    pub side_z_iface_name: String,
}

impl AcceptLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let use_onchain_allocation =
            is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation);

        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.link_pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        if link.status != LinkStatus::Requested {
            return Err(eyre::eyre!("Link is not in Requested status"));
        }

        let (_, device_z) = GetDeviceCommand {
            pubkey_or_code: link.side_z_pk.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device Z not found"))?;

        let mut accounts = vec![
            AccountMeta::new(self.link_pubkey, false),
            AccountMeta::new(device_z.contributor_pk, false),
            AccountMeta::new(link.side_z_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if use_onchain_allocation {
            accounts.push(AccountMeta::new(link.side_a_pk, false));

            let (device_tunnel_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::DeviceTunnelBlock,
            );
            let (link_ids_ext, _, _) =
                get_resource_extension_pda(&client.get_program_id(), ResourceType::LinkIds);

            accounts.push(AccountMeta::new(device_tunnel_block_ext, false));
            accounts.push(AccountMeta::new(link_ids_ext, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
                side_z_iface_name: self.side_z_iface_name.clone(),
                use_onchain_allocation,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::link::accept::AcceptLinkCommand, tests::utils::create_test_client,
        DoubleZeroClient, MockDoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda},
        processors::link::accept::LinkAcceptArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            feature_flags::FeatureFlag,
            globalstate::GlobalState,
            link::{Link, LinkDesiredStatus, LinkHealth, LinkLinkType, LinkStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_link_accept_legacy() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let link_pubkey = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();
        let contributor_pk = Pubkey::new_unique();

        let link = Link {
            account_type: AccountType::Link,
            owner: client.get_payer(),
            index: 1,
            bump_seed: 0,
            code: "test".to_string(),
            link_type: LinkLinkType::DZX,
            link_health: LinkHealth::Unknown,
            contributor_pk,
            side_a_pk,
            side_z_pk,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: "".to_string(),
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1000000,
            delay_override_ns: 0,
            jitter_ns: 100000,
            status: LinkStatus::Requested,
            desired_status: LinkDesiredStatus::Activated,
        };

        let device_z = doublezero_serviceability::state::device::Device {
            contributor_pk,
            ..Default::default()
        };

        // Mock Link fetch
        client
            .expect_get()
            .with(predicate::eq(link_pubkey))
            .returning(move |_| Ok(AccountData::Link(link.clone())));

        // Mock Device Z fetch
        client
            .expect_get()
            .with(predicate::eq(side_z_pk))
            .returning(move |_| Ok(AccountData::Device(device_z.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
                    side_z_iface_name: "Ethernet1".to_string(),
                    use_onchain_allocation: false,
                })),
                predicate::eq(vec![
                    AccountMeta::new(link_pubkey, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(side_z_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = AcceptLinkCommand {
            link_pubkey,
            side_z_iface_name: "Ethernet1".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_link_accept_with_onchain_allocation() {
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
        let link_pubkey = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();
        let contributor_pk = Pubkey::new_unique();

        let link = Link {
            account_type: AccountType::Link,
            owner: client.get_payer(),
            index: 1,
            bump_seed: 0,
            code: "test".to_string(),
            link_type: LinkLinkType::DZX,
            link_health: LinkHealth::Unknown,
            contributor_pk,
            side_a_pk,
            side_z_pk,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: "".to_string(),
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1000000,
            delay_override_ns: 0,
            jitter_ns: 100000,
            status: LinkStatus::Requested,
            desired_status: LinkDesiredStatus::Activated,
        };

        let device_z = doublezero_serviceability::state::device::Device {
            contributor_pk,
            ..Default::default()
        };

        // Compute ResourceExtension PDAs
        let (device_tunnel_block_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::DeviceTunnelBlock);
        let (link_ids_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::LinkIds);

        // Mock Link fetch
        client
            .expect_get()
            .with(predicate::eq(link_pubkey))
            .returning(move |_| Ok(AccountData::Link(link.clone())));

        // Mock Device Z fetch
        client
            .expect_get()
            .with(predicate::eq(side_z_pk))
            .returning(move |_| Ok(AccountData::Device(device_z.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
                    side_z_iface_name: "Ethernet1".to_string(),
                    use_onchain_allocation: true,
                })),
                predicate::eq(vec![
                    AccountMeta::new(link_pubkey, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(side_z_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(side_a_pk, false),
                    AccountMeta::new(device_tunnel_block_ext, false),
                    AccountMeta::new(link_ids_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = AcceptLinkCommand {
            link_pubkey,
            side_z_iface_name: "Ethernet1".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
