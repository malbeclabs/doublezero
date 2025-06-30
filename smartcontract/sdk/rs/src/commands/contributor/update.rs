use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::contributor::update::ContributorUpdateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateContributorCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub ata_owner: Option<Pubkey>,
}

impl UpdateContributorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::UpdateContributor(ContributorUpdateArgs {
                code: self.code.to_owned(),
                ata_owner_pk: self.ata_owner,
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::contributor::update::UpdateContributorCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_contributor_pda, get_globalstate_pda},
        processors::contributor::update::ContributorUpdateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{
        instruction::AccountMeta, pubkey::Pubkey, signature::Signature, system_program,
    };

    #[test]
    fn test_commands_contributor_update_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_contributor_pda(&client.get_program_id(), 1);
        let payer = client.get_payer();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateContributor(
                    ContributorUpdateArgs {
                        code: Some("test".to_string()),
                        ata_owner_pk: Some(Pubkey::default()),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(payer, true),
                    AccountMeta::new(system_program::id(), false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = UpdateContributorCommand {
            pubkey: pda_pubkey,
            code: Some("test".to_string()),
            ata_owner: Some(Pubkey::default()),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
