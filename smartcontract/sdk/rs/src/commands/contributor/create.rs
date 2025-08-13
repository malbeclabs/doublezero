use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_contributor_pda,
    processors::contributor::create::ContributorCreateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateContributorCommand {
    pub code: String,
    pub owner: Pubkey,
}

impl CreateContributorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_contributor_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateContributor(ContributorCreateArgs { code }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.owner, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::contributor::create::CreateContributorCommand, tests::utils::create_test_client,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction, processors::contributor::create::ContributorCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_contributor_create_command() {
        let mut client = create_test_client();

        let owner = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateContributor(
                    ContributorCreateArgs {
                        code: "test".to_string(),
                    },
                )),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

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
