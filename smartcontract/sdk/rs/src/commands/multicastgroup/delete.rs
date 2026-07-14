use crate::{commands::multicastgroup::get::GetMulticastGroupCommand, DoubleZeroClient};
use doublezero_serviceability::processors::multicastgroup::delete::MulticastGroupDeleteArgs;
use doublezero_serviceability_instruction::multicastgroup::delete_multicast_group;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteMulticastGroupCommand {
    pub pubkey: Pubkey,
}

impl DeleteMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (_, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("MulticastGroup not found"))?;

        client.send_transaction(delete_multicast_group(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            &mgroup.owner,
            MulticastGroupDeleteArgs {
                use_onchain_deallocation: true,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::delete::DeleteMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_multicastgroup_pda,
        processors::multicastgroup::delete::MulticastGroupDeleteArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
        },
    };
    use doublezero_serviceability_instruction::multicastgroup::delete_multicast_group;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    fn make_test_mgroup(owner: Pubkey, bump_seed: u8) -> MulticastGroup {
        MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 2,
            bump_seed,
            tenant_pk: Pubkey::default(),
            code: "mg01".to_string(),
            multicast_ip: Ipv4Addr::UNSPECIFIED,
            max_bandwidth: 0,
            status: MulticastGroupStatus::Activated,
            owner,
            publisher_count: 1,
            subscriber_count: 0,
        }
    }

    #[test]
    fn test_commands_multicastgroup_delete() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, mgroup_bump) = get_multicastgroup_pda(&program_id, 1);
        let owner = Pubkey::new_unique();
        let mgroup = make_test_mgroup(owner, mgroup_bump);

        let mgroup_cloned = mgroup.clone();
        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup_cloned.clone())));

        let expected = delete_multicast_group(
            &program_id,
            &payer,
            &pda_pubkey,
            &owner,
            MulticastGroupDeleteArgs {
                use_onchain_deallocation: true,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = DeleteMulticastGroupCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
