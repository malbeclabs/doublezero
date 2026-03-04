use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, link::get::GetLinkCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::link::closeaccount::LinkCloseAccountArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CloseAccountLinkCommand {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
    /// When true, SDK computes ResourceExtension PDAs and includes them for on-chain deallocation.
    /// When false, uses legacy behavior without resource deallocation.
    pub use_onchain_deallocation: bool,
}

impl CloseAccountLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(self.owner, false),
            AccountMeta::new(link.contributor_pk, false),
            AccountMeta::new(link.side_a_pk, false),
            AccountMeta::new(link.side_z_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if self.use_onchain_deallocation {
            // Global DeviceTunnelBlock (for tunnel_net deallocation)
            let (device_tunnel_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::DeviceTunnelBlock,
            );

            // Global LinkIds (for tunnel_id deallocation)
            let (link_ids_ext, _, _) =
                get_resource_extension_pda(&client.get_program_id(), ResourceType::LinkIds);

            accounts.push(AccountMeta::new(device_tunnel_block_ext, false));
            accounts.push(AccountMeta::new(link_ids_ext, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::CloseAccountLink(LinkCloseAccountArgs {
                use_onchain_deallocation: self.use_onchain_deallocation,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::link::closeaccount::CloseAccountLinkCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda},
        processors::link::closeaccount::LinkCloseAccountArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            link::{Link, LinkDesiredStatus, LinkHealth, LinkLinkType, LinkStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_link_closeaccount_without_resource_extension() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let link_pubkey = Pubkey::new_unique();
        let owner = client.get_payer();
        let contributor_pk = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();

        let link = Link {
            account_type: AccountType::Link,
            owner,
            index: 1,
            bump_seed: 0,
            code: "test".to_string(),
            link_type: LinkLinkType::DZX,
            link_health: LinkHealth::Unknown,
            contributor_pk,
            side_a_pk,
            side_z_pk,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: "Ethernet1".to_string(),
            tunnel_id: 500,
            tunnel_net: "10.0.0.0/21".parse().unwrap(),
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1000000,
            delay_override_ns: 0,
            jitter_ns: 100000,
            status: LinkStatus::Deleting,
            desired_status: LinkDesiredStatus::Activated,
        };

        // Mock Link fetch
        client
            .expect_get()
            .with(predicate::eq(link_pubkey))
            .returning(move |_| Ok(AccountData::Link(link.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CloseAccountLink(
                    LinkCloseAccountArgs {
                        use_onchain_deallocation: false,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(link_pubkey, false),
                    AccountMeta::new(owner, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(side_a_pk, false),
                    AccountMeta::new(side_z_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CloseAccountLinkCommand {
            pubkey: link_pubkey,
            owner,
            use_onchain_deallocation: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_link_closeaccount_with_onchain_deallocation() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let link_pubkey = Pubkey::new_unique();
        let owner = client.get_payer();
        let contributor_pk = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();

        let link = Link {
            account_type: AccountType::Link,
            owner,
            index: 1,
            bump_seed: 0,
            code: "test".to_string(),
            link_type: LinkLinkType::DZX,
            link_health: LinkHealth::Unknown,
            contributor_pk,
            side_a_pk,
            side_z_pk,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: "Ethernet1".to_string(),
            tunnel_id: 500,
            tunnel_net: "10.0.0.0/21".parse().unwrap(),
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1000000,
            delay_override_ns: 0,
            jitter_ns: 100000,
            status: LinkStatus::Deleting,
            desired_status: LinkDesiredStatus::Activated,
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

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CloseAccountLink(
                    LinkCloseAccountArgs {
                        use_onchain_deallocation: true,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(link_pubkey, false),
                    AccountMeta::new(owner, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(side_a_pk, false),
                    AccountMeta::new(side_z_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(device_tunnel_block_ext, false),
                    AccountMeta::new(link_ids_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CloseAccountLinkCommand {
            pubkey: link_pubkey,
            owner,
            use_onchain_deallocation: true,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
