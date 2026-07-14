use crate::{
    commands::multicastgroup::{allowlist::resolve_accesspass_pda, get::GetMulticastGroupCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::processors::multicastgroup::allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs;
use doublezero_serviceability_instruction::multicastgroup::add_multicast_group_pub_allowlist;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct AddMulticastGroupPubAllowlistCommand {
    pub pubkey_or_code: String,
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl AddMulticastGroupPubAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (mgroup_pubkey, _mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey_or_code.clone(),
        }
        .execute(client)?;

        let accesspass_pk = resolve_accesspass_pda(client, &self.client_ip, &self.user_payer);

        client.send_transaction(add_multicast_group_pub_allowlist(
            &client.get_program_id(),
            &client.get_payer(),
            &mgroup_pubkey,
            &accesspass_pk,
            &self.user_payer,
            AddMulticastGroupPubAllowlistArgs {
                client_ip: self.client_ip,
                user_payer: self.user_payer,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::allowlist::publisher::add::AddMulticastGroupPubAllowlistCommand,
        tests::utils::create_test_client,
    };
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_allowlist_publisher_add() {
        let mut client = create_test_client();

        let pubkey = Pubkey::new_unique();
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 1,
            owner: Pubkey::new_unique(),
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 1].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            code: "test_code".to_string(),
            publisher_count: 5,
            subscriber_count: 10,
        };

        let cloned_mgroup = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(cloned_mgroup.clone())));
        // AccessPass PDA lookups in resolve_accesspass_pda — no account found, use static PDA
        client
            .expect_get()
            .returning(|_| Err(eyre::eyre!("account not found")));
        let cloned_mgroup = mgroup.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::MulticastGroup))
            .returning(move |_| {
                let mut map = std::collections::HashMap::new();
                map.insert(pubkey, AccountData::MulticastGroup(cloned_mgroup.clone()));
                Ok(map)
            });
        client
            .expect_send_transaction()
            .with(predicate::always())
            .returning(|_| Ok(Signature::new_unique()));

        let res = AddMulticastGroupPubAllowlistCommand {
            pubkey_or_code: "test_code".to_string(),
            client_ip: [192, 168, 1, 1].into(),
            user_payer: pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
