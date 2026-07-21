use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    pda::get_contributor_pda, processors::contributor::create::ContributorCreateArgs,
};
use doublezero_serviceability_instruction::contributor::create_contributor;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateContributorCommand {
    pub code: String,
    pub owner: Pubkey,
}

impl CreateContributorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (_, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let program_id = client.get_program_id();
        let account_index = globalstate.account_index + 1;
        let (pda_pubkey, _) = get_contributor_pda(&program_id, account_index);

        let ix = create_contributor(
            &program_id,
            &client.get_payer(),
            &self.owner,
            account_index,
            ContributorCreateArgs { code },
        );

        client.send_transaction(ix).map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::contributor::create::CreateContributorCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::contributor::create::ContributorCreateArgs;
    use doublezero_serviceability_instruction::contributor::create_contributor;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_contributor_create_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let owner = Pubkey::new_unique();

        let expected = create_contributor(
            &program_id,
            &payer,
            &owner,
            1,
            ContributorCreateArgs {
                code: "test".to_string(),
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CreateContributorCommand {
            code: "test/invalid".to_string(),
            owner: Pubkey::default(),
        }
        .execute(&client);

        assert!(res.is_err());

        let res = CreateContributorCommand {
            code: "test whitespace".to_string(),
            owner: Pubkey::default(),
        }
        .execute(&client);

        assert!(res.is_err());

        let res = CreateContributorCommand {
            code: "test".to_string(),
            owner,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
