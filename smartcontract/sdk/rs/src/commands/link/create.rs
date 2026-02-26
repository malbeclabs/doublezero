use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_link_pda, get_resource_extension_pda},
    processors::link::create::LinkCreateArgs,
    resource::ResourceType,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        link::{LinkDesiredStatus, LinkLinkType},
    },
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateLinkCommand {
    pub code: String,
    pub contributor_pk: Pubkey,
    pub desired_status: Option<LinkDesiredStatus>,
    pub side_a_pk: Pubkey,
    pub side_z_pk: Pubkey,
    pub link_type: LinkLinkType,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay_ns: u64,
    pub jitter_ns: u64,
    pub side_a_iface_name: String,
    pub side_z_iface_name: Option<String>,
}

impl CreateLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let use_onchain_allocation =
            is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation);

        let (pda_pubkey, _) = get_link_pda(&client.get_program_id(), globalstate.account_index + 1);

        let mut accounts = vec![
            AccountMeta::new(pda_pubkey, false),
            AccountMeta::new(self.contributor_pk, false),
            AccountMeta::new(self.side_a_pk, false),
            AccountMeta::new(self.side_z_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if use_onchain_allocation {
            let (device_tunnel_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::DeviceTunnelBlock,
            );
            let (link_ids_ext, _, _) =
                get_resource_extension_pda(&client.get_program_id(), ResourceType::LinkIds);

            accounts.push(AccountMeta::new(device_tunnel_block_ext, false));
            accounts.push(AccountMeta::new(link_ids_ext, false));
        }

        client
            .execute_transaction(
                DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                    code,
                    link_type: self.link_type,
                    desired_status: self.desired_status,
                    bandwidth: self.bandwidth,
                    mtu: self.mtu,
                    delay_ns: self.delay_ns,
                    jitter_ns: self.jitter_ns,
                    side_a_iface_name: self.side_a_iface_name.clone(),
                    side_z_iface_name: self.side_z_iface_name.clone(),
                    use_onchain_allocation,
                }),
                accounts,
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::link::create::CreateLinkCommand, tests::utils::create_test_client,
        DoubleZeroClient, MockDoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_link_pda, get_resource_extension_pda},
        processors::link::create::LinkCreateArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData, accounttype::AccountType, feature_flags::FeatureFlag,
            globalstate::GlobalState,
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_link_create_legacy() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let (pda_pubkey, _) = get_link_pda(&program_id, 1);
        let contributor_pk = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                    code: "test".to_string(),
                    link_type: doublezero_serviceability::state::link::LinkLinkType::DZX,
                    desired_status: None,
                    bandwidth: 10_000_000_000,
                    mtu: 9000,
                    delay_ns: 1000000,
                    jitter_ns: 100000,
                    side_a_iface_name: "Ethernet0".to_string(),
                    side_z_iface_name: Some("Ethernet1".to_string()),
                    use_onchain_allocation: false,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(side_a_pk, false),
                    AccountMeta::new(side_z_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateLinkCommand {
            code: "test".to_string(),
            contributor_pk,
            desired_status: None,
            side_a_pk,
            side_z_pk,
            link_type: doublezero_serviceability::state::link::LinkLinkType::DZX,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1000000,
            jitter_ns: 100000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_link_create_with_onchain_allocation() {
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
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let (pda_pubkey, _) = get_link_pda(&program_id, 1);
        let contributor_pk = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();

        let (device_tunnel_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
        let (link_ids_ext, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                    code: "test".to_string(),
                    link_type: doublezero_serviceability::state::link::LinkLinkType::DZX,
                    desired_status: None,
                    bandwidth: 10_000_000_000,
                    mtu: 9000,
                    delay_ns: 1000000,
                    jitter_ns: 100000,
                    side_a_iface_name: "Ethernet0".to_string(),
                    side_z_iface_name: Some("Ethernet1".to_string()),
                    use_onchain_allocation: true,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(side_a_pk, false),
                    AccountMeta::new(side_z_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(device_tunnel_block_ext, false),
                    AccountMeta::new(link_ids_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateLinkCommand {
            code: "test".to_string(),
            contributor_pk,
            desired_status: None,
            side_a_pk,
            side_z_pk,
            link_type: doublezero_serviceability::state::link::LinkLinkType::DZX,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1000000,
            jitter_ns: 100000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
