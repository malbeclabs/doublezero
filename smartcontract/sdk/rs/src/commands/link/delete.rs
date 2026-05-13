use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, link::get::GetLinkCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::link::delete::LinkDeleteArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteLinkCommand {
    pub pubkey: Pubkey,
}

impl DeleteLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        let (device_tunnel_block_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::DeviceTunnelBlock);
        let (link_ids_ext, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::LinkIds);
        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(link.contributor_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(link.side_a_pk, false),
            AccountMeta::new(link.side_z_pk, false),
            AccountMeta::new(device_tunnel_block_ext, false),
            AccountMeta::new(link_ids_ext, false),
            AccountMeta::new(link.owner, false),
        ];
        // Topology accounts for reference_count decrement — one writable account
        // per entry in link.link_topologies.
        for topology_pk in &link.link_topologies {
            accounts.push(AccountMeta::new(*topology_pk, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
                use_onchain_deallocation: true,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::link::delete::DeleteLinkCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda},
        processors::link::delete::LinkDeleteArgs,
        resource::ResourceType,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            link::{Link, LinkDesiredStatus, LinkHealth, LinkLinkType, LinkStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    fn make_test_link(owner: Pubkey, side_a_pk: Pubkey, side_z_pk: Pubkey) -> Link {
        Link {
            account_type: AccountType::Link,
            owner,
            index: 1,
            bump_seed: 0,
            code: "test".to_string(),
            link_type: LinkLinkType::DZX,
            link_health: LinkHealth::Unknown,
            contributor_pk: Pubkey::new_unique(),
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
            status: LinkStatus::Activated,
            desired_status: LinkDesiredStatus::Activated,
            link_topologies: vec![],
            link_flags: 0,
        }
    }

    #[test]
    fn test_commands_link_delete() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let (device_tunnel_block_ext, _, _) =
            get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
        let (link_ids_ext, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
        let link_pubkey = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();
        let link = make_test_link(client.get_payer(), side_a_pk, side_z_pk);
        let contributor_pk = link.contributor_pk;
        let owner = link.owner;

        client
            .expect_get()
            .with(predicate::eq(link_pubkey))
            .returning(move |_| Ok(AccountData::Link(link.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {
                    use_onchain_deallocation: true,
                })),
                predicate::eq(vec![
                    AccountMeta::new(link_pubkey, false),
                    AccountMeta::new(contributor_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(side_a_pk, false),
                    AccountMeta::new(side_z_pk, false),
                    AccountMeta::new(device_tunnel_block_ext, false),
                    AccountMeta::new(link_ids_ext, false),
                    AccountMeta::new(owner, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteLinkCommand {
            pubkey: link_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
