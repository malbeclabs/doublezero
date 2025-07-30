use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::normalize_account_code;
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
        let code = self.validate_code()?;
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_contributor_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
                    code,
                    owner: self.owner,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.owner, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }

    fn validate_code(&self) -> eyre::Result<String> {
        normalize_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::contributor::create::CreateContributorCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_contributor_pda, get_globalstate_pda},
        processors::contributor::create::ContributorCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_contributor_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_contributor_pda(&client.get_program_id(), 1);
        let owner = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateContributor(
                    ContributorCreateArgs {
                        code: "test_whitespace".to_string(),
                        owner: Pubkey::default(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateContributor(
                    ContributorCreateArgs {
                        code: "test".to_string(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(owner, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
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

        assert!(res.is_ok());

        let res = CreateContributorCommand {
            code: "test".to_string(),
            owner,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
