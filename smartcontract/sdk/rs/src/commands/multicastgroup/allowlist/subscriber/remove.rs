use std::net::Ipv4Addr;

use doublezero_serviceability::processors::multicastgroup::allowlist::subscriber::remove::RemoveMulticastGroupSubAllowlistArgs;
use doublezero_serviceability_instruction::multicastgroup::remove_multicast_group_sub_allowlist;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::{
    commands::multicastgroup::{allowlist::resolve_accesspass_pda, get::GetMulticastGroupCommand},
    DoubleZeroClient,
};

#[derive(Debug, PartialEq, Clone)]
pub struct RemoveMulticastGroupSubAllowlistCommand {
    pub pubkey_or_code: String,
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl RemoveMulticastGroupSubAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, _mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey_or_code.clone(),
        }
        .execute(client)?;

        let accesspass_pk = resolve_accesspass_pda(client, &self.client_ip, &self.user_payer);

        client.send_transaction(remove_multicast_group_sub_allowlist(
            &client.get_program_id(),
            &client.get_payer(),
            &pda_pubkey,
            &accesspass_pk,
            RemoveMulticastGroupSubAllowlistArgs {
                client_ip: self.client_ip,
                user_payer: self.user_payer,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::allowlist::subscriber::remove::RemoveMulticastGroupSubAllowlistCommand,
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
    fn test_commands_multicastgroup_allowlist_subscriber_remove() {
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

        // remove with valid code
        let res = RemoveMulticastGroupSubAllowlistCommand {
            pubkey_or_code: "test_code".to_string(),
            client_ip: [192, 168, 1, 1].into(),
            user_payer: pubkey,
        }
        .execute(&client);
        assert!(res.is_ok());

        // error removing with invalid code character
        let res = RemoveMulticastGroupSubAllowlistCommand {
            pubkey_or_code: "test%code".to_string(),
            client_ip: [192, 168, 1, 1].into(),
            user_payer: pubkey,
        }
        .execute(&client);
        assert!(res.is_err());
    }
}
