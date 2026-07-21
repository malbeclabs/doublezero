use crate::{commands::link::get::GetLinkCommand, DoubleZeroClient};
use doublezero_serviceability::processors::link::delete::LinkDeleteArgs;
use doublezero_serviceability_instruction::link::delete_link;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteLinkCommand {
    pub pubkey: Pubkey,
}

impl DeleteLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        client.send_transaction(delete_link(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            &link.contributor_pk,
            &link.side_a_pk,
            &link.side_z_pk,
            &link.owner,
            &link.link_topologies,
            LinkDeleteArgs {
                use_onchain_deallocation: true,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::link::delete::DeleteLinkCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        processors::link::delete::LinkDeleteArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            link::{Link, LinkDesiredStatus, LinkHealth, LinkLinkType, LinkStatus},
        },
    };
    use doublezero_serviceability_instruction::link::delete_link;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

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
            delay_ns: 1_000_000,
            delay_override_ns: 0,
            jitter_ns: 100_000,
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
        let payer = client.get_payer();
        let link_pubkey = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();
        let link = make_test_link(payer, side_a_pk, side_z_pk);
        let contributor_pk = link.contributor_pk;
        let owner = link.owner;

        client
            .expect_get()
            .with(predicate::eq(link_pubkey))
            .returning(move |_| Ok(AccountData::Link(link.clone())));

        let expected = delete_link(
            &program_id,
            &payer,
            &link_pubkey,
            &contributor_pk,
            &side_a_pk,
            &side_z_pk,
            &owner,
            &[],
            LinkDeleteArgs {
                use_onchain_deallocation: true,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = DeleteLinkCommand {
            pubkey: link_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
