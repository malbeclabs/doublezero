use crate::DoubleZeroClient;
use doublezero_serviceability_instruction::feed::halt_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// Stop a feed publishing. The builder's own lever, and an operator's.
#[derive(Debug, PartialEq, Clone)]
pub struct HaltFeedCommand {
    pub pubkey: Pubkey,
}

impl HaltFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let ix = halt_feed(&client.get_program_id(), &client.get_payer(), &self.pubkey);

        client.send_transaction(ix)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::feed::halt::HaltFeedCommand, tests::utils::create_test_client, DoubleZeroClient,
    };

    use doublezero_serviceability_instruction::feed::halt_feed;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    /// Halting is authorized, unlike finalizing. The `Permission` account goes last, after the accounts the instruction builder names,
    /// because that is where `authorize` reads it.
    #[test]
    fn test_commands_feed_halt_appends_the_payer_permission_account() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let expected = halt_feed(&program_id, &payer, &feed_pk);

        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .times(1)
            .returning(move |_| Ok(signature));

        let res = HaltFeedCommand { pubkey: feed_pk }.execute(&client);
        assert_eq!(res.unwrap(), signature);
    }
}
