use crate::{
    commands::{feed::get::GetFeedCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::feed::delete::FeedDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteFeedCommand {
    pub pubkey: Pubkey,
}

impl DeleteFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, feed) = GetFeedCommand {
            pubkey_or_code: self.pubkey.to_string(),
            exchange: None,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Feed not found"))?;

        if feed.reference_count > 0 {
            return Err(eyre::eyre!(
                "Feed cannot be deleted, it has {} references",
                feed.reference_count
            ));
        }

        // Accounts: [feed, globalstate, (payer, system appended by client)].
        client.execute_transaction(
            DoubleZeroInstruction::DeleteFeed(FeedDeleteArgs {}),
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
        commands::feed::delete::DeleteFeedCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_feed_pda, get_globalstate_pda},
        processors::feed::delete::FeedDeleteArgs,
        state::{accountdata::AccountData, accounttype::AccountType, feed::Feed},
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_feed_delete_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) =
            get_feed_pda(&client.get_program_id(), "test_feed", &Pubkey::new_unique());
        let feed = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::default(),
            bump_seed: 255,
            code: "test_feed".to_string(),
            name: "Test Feed".to_string(),
            reference_count: 0,
            exchange: Pubkey::new_unique(),
            groups: vec![Pubkey::new_unique()],
        };

        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::Feed(feed.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteFeed(FeedDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteFeedCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
