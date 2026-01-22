use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, link::get::GetLinkCommand},
    DoubleZeroClient,
};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::link::activate::LinkActivateArgs, resource::ResourceType, state::link::LinkStatus,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateLinkCommand {
    pub link_pubkey: Pubkey,
    pub side_a_pk: Pubkey,
    pub side_z_pk: Pubkey,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    /// When true, SDK computes ResourceExtension PDAs and includes them for on-chain allocation.
    /// When false, uses legacy behavior with caller-provided tunnel_id and tunnel_net.
    pub use_onchain_allocation: bool,
}

impl ActivateLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.link_pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        if link.status != LinkStatus::Pending {
            return Err(eyre::eyre!("Link is not in Pending status"));
        }

        // Build accounts list with optional ResourceExtension accounts before payer
        // (payer and system_program are appended by execute_transaction)
        let mut accounts = vec![
            AccountMeta::new(self.link_pubkey, false),
            AccountMeta::new(self.side_a_pk, false),
            AccountMeta::new(self.side_z_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if self.use_onchain_allocation {
            // Global DeviceTunnelBlock (for tunnel_net allocation)
            let (device_tunnel_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::DeviceTunnelBlock,
            );

            // Global LinkIds (for tunnel_id allocation)
            let (link_ids_ext, _, _) =
                get_resource_extension_pda(&client.get_program_id(), ResourceType::LinkIds);

            accounts.push(AccountMeta::new(device_tunnel_block_ext, false));
            accounts.push(AccountMeta::new(link_ids_ext, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
                use_onchain_allocation: self.use_onchain_allocation,
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::link::activate::ActivateLinkCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda},
        processors::link::activate::LinkActivateArgs,
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
    fn test_commands_link_activate_without_resource_extension() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let link_pubkey = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();
        let tunnel_id: u16 = 500;
        let tunnel_net: NetworkV4 = "10.0.0.0/21".parse().unwrap();

        let link = Link {
            account_type: AccountType::Link,
            owner: client.get_payer(),
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
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1000000,
            delay_override_ns: 0,
            jitter_ns: 100000,
            status: LinkStatus::Pending,
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
                predicate::eq(DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                    tunnel_id,
                    tunnel_net,
                    use_onchain_allocation: false,
                })),
                predicate::eq(vec![
                    AccountMeta::new(link_pubkey, false),
                    AccountMeta::new(side_a_pk, false),
                    AccountMeta::new(side_z_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ActivateLinkCommand {
            link_pubkey,
            side_a_pk,
            side_z_pk,
            tunnel_id,
            tunnel_net,
            use_onchain_allocation: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_link_activate_with_onchain_allocation() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let link_pubkey = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();

        let link = Link {
            account_type: AccountType::Link,
            owner: client.get_payer(),
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
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1000000,
            delay_override_ns: 0,
            jitter_ns: 100000,
            status: LinkStatus::Pending,
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
                predicate::eq(DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                    tunnel_id: 0,
                    tunnel_net: NetworkV4::default(),
                    use_onchain_allocation: true,
                })),
                predicate::eq(vec![
                    AccountMeta::new(link_pubkey, false),
                    AccountMeta::new(side_a_pk, false),
                    AccountMeta::new(side_z_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(device_tunnel_block_ext, false),
                    AccountMeta::new(link_ids_ext, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ActivateLinkCommand {
            link_pubkey,
            side_a_pk,
            side_z_pk,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            use_onchain_allocation: true,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
