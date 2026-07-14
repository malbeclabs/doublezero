use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{pda::get_feed_pda, processors::feed::create::FeedCreateArgs};
use doublezero_serviceability_instruction::feed::create_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct CreateFeedCommand {
    pub code: String,
    pub name: String,
    /// The metro (exchange) this feed serves; part of the PDA seed.
    pub exchange: Pubkey,
    /// Multicast groups joinable in this metro.
    pub groups: Vec<Pubkey>,
}

impl CreateFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let program_id = client.get_program_id();
        let (pda_pubkey, _) = get_feed_pda(&program_id, &code, &self.exchange);

        // The builder derives the feed and globalstate PDAs.
        client
            .send_transaction(create_feed(
                &program_id,
                &client.get_payer(),
                FeedCreateArgs {
                    code,
                    name: self.name.clone(),
                    exchange: self.exchange,
                    groups: self.groups.clone(),
                },
            ))
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::feed::create::CreateFeedCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::feed::create::FeedCreateArgs;
    use doublezero_serviceability_instruction::feed::create_feed;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_feed_create_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let exchange = Pubkey::new_unique();
        let group = Pubkey::new_unique();

        let expected = create_feed(
            &program_id,
            &payer,
            FeedCreateArgs {
                code: "test_feed".to_string(),
                name: "Test Feed".to_string(),
                exchange,
                groups: vec![group],
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let create_command = CreateFeedCommand {
            code: "test_feed".to_string(),
            name: "Test Feed".to_string(),
            exchange,
            groups: vec![group],
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
