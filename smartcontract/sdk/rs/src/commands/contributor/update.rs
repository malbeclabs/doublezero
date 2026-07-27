use crate::DoubleZeroClient;
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::processors::contributor::update::ContributorUpdateArgs;
use doublezero_serviceability_instruction::contributor::update_contributor;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateContributorCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub owner: Option<Pubkey>,
    pub ops_manager_pk: Option<Pubkey>,
}

impl UpdateContributorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let code = self
            .code
            .as_ref()
            .map(|code| validate_account_code(code))
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        client.send_transaction(update_contributor(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            ContributorUpdateArgs {
                code,
                owner: self.owner.to_owned(),
                ops_manager_pk: self.ops_manager_pk.to_owned(),
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::contributor::update::UpdateContributorCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_contributor_pda, processors::contributor::update::ContributorUpdateArgs,
    };
    use doublezero_serviceability_instruction::contributor::update_contributor;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_contributor_update_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_contributor_pda(&program_id, 1);

        let expected = update_contributor(
            &program_id,
            &payer,
            &pda_pubkey,
            ContributorUpdateArgs {
                code: Some("test".to_string()),
                owner: Some(Pubkey::default()),
                ops_manager_pk: Some(Pubkey::default()),
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateContributorCommand {
            pubkey: pda_pubkey,
            code: Some("test".to_string()),
            owner: Some(Pubkey::default()),
            ops_manager_pk: Some(Pubkey::default()),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
