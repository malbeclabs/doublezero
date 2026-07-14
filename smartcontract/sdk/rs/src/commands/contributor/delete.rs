use crate::{commands::contributor::get::GetContributorCommand, DoubleZeroClient};
use doublezero_serviceability::processors::contributor::delete::ContributorDeleteArgs;
use doublezero_serviceability_instruction::contributor::delete_contributor;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteContributorCommand {
    pub pubkey: Pubkey,
}

impl DeleteContributorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (_, contributor) = GetContributorCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Contributor not found"))?;

        if contributor.reference_count > 0 {
            return Err(eyre::eyre!(
                "Contributor cannot be deleted, it has {} references",
                contributor.reference_count
            ));
        }

        client.send_transaction(delete_contributor(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            ContributorDeleteArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::contributor::delete::DeleteContributorCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_contributor_pda,
        processors::contributor::delete::ContributorDeleteArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            contributor::{Contributor, ContributorStatus},
        },
    };
    use doublezero_serviceability_instruction::contributor::delete_contributor;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_contributor_delete_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_contributor_pda(&program_id, 1);
        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 255,
            code: "cont".to_string(),
            status: ContributorStatus::Activated,
            reference_count: 0,
            owner: Pubkey::default(),
            ops_manager_pk: Pubkey::default(),
        };

        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::Contributor(contributor.clone())));

        let expected =
            delete_contributor(&program_id, &payer, &pda_pubkey, ContributorDeleteArgs {});
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = DeleteContributorCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
