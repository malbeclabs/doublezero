use crate::{AccountData, DoubleZeroClient};
use doublezero_serviceability::processors::feed::delete::FeedDeleteArgs;
use doublezero_serviceability_instruction::feed::delete_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteFeedCommand {
    pub pubkey: Pubkey,
}

impl DeleteFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let AccountData::Feed(feed) = client.get(self.pubkey)? else {
            eyre::bail!("{} is not a feed", self.pubkey);
        };
        let ix = delete_feed(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            &feed.groups,
            FeedDeleteArgs {},
        );

        client.send_transaction(ix)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::feed::delete::DeleteFeedCommand, tests::utils::create_test_client, AccountData,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_feed_pda,
        processors::feed::delete::FeedDeleteArgs,
        state::{accounttype::AccountType, feed::Feed},
    };
    use doublezero_serviceability_instruction::feed::delete_feed;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_feed_delete_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_feed_pda(&program_id, "test_feed", &Pubkey::new_unique());

        let group = Pubkey::new_unique();
        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| {
                Ok(AccountData::Feed(Feed {
                    account_type: AccountType::Feed,
                    groups: vec![group],
                    ..Default::default()
                }))
            });

        let expected = delete_feed(
            &program_id,
            &payer,
            &pda_pubkey,
            &[group],
            FeedDeleteArgs {},
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = DeleteFeedCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
