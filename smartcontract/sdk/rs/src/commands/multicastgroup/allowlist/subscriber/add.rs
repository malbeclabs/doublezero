use std::net::Ipv4Addr;

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_accesspass_pda, get_globalstate_pda},
    processors::multicastgroup::allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::multicastgroup::get::GetMulticastGroupCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct AddMulticastGroupSubAllowlistCommand {
    pub pubkey_or_code: String,
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl AddMulticastGroupSubAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, _mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey_or_code.clone(),
        }
        .execute(client)?;

        let (accesspass_pk, _) =
            get_accesspass_pda(&client.get_program_id(), &self.client_ip, &self.user_payer);

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
                    client_ip: self.client_ip,
                    user_payer: self.user_payer,
                },
            ),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(accesspass_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::allowlist::subscriber::add::AddMulticastGroupSubAllowlistCommand,
        tests::utils::create_test_client,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        processors::multicastgroup::allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_allowlist_subscriber_add() {
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
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                    AddMulticastGroupSubAllowlistArgs {
                        client_ip: [192, 168, 1, 1].into(),
                        user_payer: pubkey,
                    },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        // add using valid code
        let res = AddMulticastGroupSubAllowlistCommand {
            pubkey_or_code: "test_code".to_string(),
            client_ip: [192, 168, 1, 1].into(),
            user_payer: pubkey,
        }
        .execute(&client);
        assert!(res.is_ok());

        // error attempting to add code with invalid char(s)
        let res = AddMulticastGroupSubAllowlistCommand {
            pubkey_or_code: "test code".to_string(),
            client_ip: [192, 168, 1, 1].into(),
            user_payer: pubkey,
        }
        .execute(&client);
        assert!(res.is_err());
    }
}
