use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_feed_pda,
    processors::feed::create::FeedCreateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateFeedCommand {
    pub code: String,
    pub name: String,
    /// `exchange_pk → group_pks`. Empty ⇒ no metro restriction.
    pub metros: Vec<(Pubkey, Vec<Pubkey>)>,
}

impl CreateFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_feed_pda(&client.get_program_id(), &code);

        // Accounts: [feed, globalstate, (payer, system appended by client)].
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
                    code,
                    name: self.name.clone(),
                    metros: self.metros.clone(),
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
        commands::feed::create::CreateFeedCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_feed_pda, get_globalstate_pda},
        processors::feed::create::FeedCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_feed_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_feed_pda(&client.get_program_id(), "test_feed");

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
                    code: "test_feed".to_string(),
                    name: "Test Feed".to_string(),
                    metros: vec![],
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let create_command = CreateFeedCommand {
            code: "test_feed".to_string(),
            name: "Test Feed".to_string(),
            metros: vec![],
        };

        let create_invalid_command = CreateFeedCommand {
            code: "test/feed".to_string(),
            ..create_command.clone()
        };

        let res = create_command.execute(&client);
        assert!(res.is_ok());

        let res = create_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
