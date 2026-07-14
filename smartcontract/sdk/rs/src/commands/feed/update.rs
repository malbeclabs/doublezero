use crate::DoubleZeroClient;
use doublezero_serviceability::processors::feed::update::FeedUpdateArgs;
use doublezero_serviceability_instruction::feed::update_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateFeedCommand {
    pub pubkey: Pubkey,
    pub name: Option<String>,
    /// Replacement multicast group set. `None` leaves the groups unchanged.
    pub groups: Option<Vec<Pubkey>>,
}

impl UpdateFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(update_feed(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            FeedUpdateArgs {
                name: self.name.clone(),
                groups: self.groups.clone(),
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::feed::update::UpdateFeedCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{pda::get_feed_pda, processors::feed::update::FeedUpdateArgs};
    use doublezero_serviceability_instruction::feed::update_feed;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_feed_update_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_feed_pda(&program_id, "test_feed", &Pubkey::new_unique());

        let expected = update_feed(
            &program_id,
            &payer,
            &pda_pubkey,
            FeedUpdateArgs {
                name: Some("Test Feed".to_string()),
                groups: None,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateFeedCommand {
            pubkey: pda_pubkey,
            name: Some("Test Feed".to_string()),
            groups: None,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
