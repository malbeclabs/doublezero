use crate::DoubleZeroClient;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_contributor_pda, get_globalconfig_pda},
    processors::contributor::create::ContributorCreateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateContributorCommand {
    pub index: u128,
    pub code: String,
    pub ata_owner_pk: Pubkey,
}

impl CreateContributorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_contributor_pda(&client.get_program_id(), self.index);

        client
            .execute_transaction(
                DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
                    index: self.index,
                    bump_seed,
                    code: self.code.clone(),
                    ata_owner_pk: self.ata_owner_pk,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::contributor::create::CreateContributorCommand, index::nextindex,
        tests::utils::create_test_client, DoubleZeroClient,
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

        let index = nextindex();
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_contributor_pda(&client.get_program_id(), index);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateContributor(
                    ContributorCreateArgs {
                        index,
                        bump_seed,
                        code: "test".to_string(),
                        ata_owner_pk: Pubkey::default(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateContributorCommand {
            index,
            code: "test".to_string(),
            ata_owner_pk: Pubkey::default(),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
