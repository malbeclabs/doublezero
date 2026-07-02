use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::feed::update::FeedUpdateArgs,
    state::feed::MetroGroups,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateFeedCommand {
    pub pubkey: Pubkey,
    pub name: Option<String>,
    /// `exchange_pk → group_pks`. `None` leaves the metro map unchanged.
    pub metros: Option<Vec<MetroGroups>>,
}

impl UpdateFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        // Accounts: [feed, globalstate, (payer, system appended by client)].
        client.execute_transaction(
            DoubleZeroInstruction::UpdateFeed(FeedUpdateArgs {
                name: self.name.clone(),
                metros: self.metros.clone(),
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
        commands::feed::update::UpdateFeedCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_feed_pda, get_globalstate_pda},
        processors::feed::update::FeedUpdateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_feed_update_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_feed_pda(&client.get_program_id(), "test_feed");

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateFeed(FeedUpdateArgs {
                    name: Some("Test Feed".to_string()),
                    metros: None,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = UpdateFeedCommand {
            pubkey: pda_pubkey,
            name: Some("Test Feed".to_string()),
            metros: None,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
