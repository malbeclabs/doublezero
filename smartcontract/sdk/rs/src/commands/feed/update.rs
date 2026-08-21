use crate::{commands::common::append_payer_permission_account, DoubleZeroClient};
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
        let mut ix = update_feed(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            FeedUpdateArgs {
                name: self.name.clone(),
                groups: self.groups.clone(),
            },
        );
        append_payer_permission_account(client, &mut ix);
        client.send_transaction(ix)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::feed::update::UpdateFeedCommand,
        tests::utils::{create_test_client, expect_missing_permission_account},
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::{get_feed_pda, get_permission_pda},
        processors::feed::update::FeedUpdateArgs,
    };
    use doublezero_serviceability_instruction::feed::update_feed;
    use mockall::predicate;
    use solana_sdk::{
        account::Account, message::AccountMeta, pubkey::Pubkey, signature::Signature,
    };

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

        expect_missing_permission_account(&mut client);

        let res = UpdateFeedCommand {
            pubkey: pda_pubkey,
            name: Some("Test Feed".to_string()),
            groups: None,
        }
        .execute(&client);
        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_feed_update_command_with_permission_pda() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_feed_pda(&program_id, "test_feed", &Pubkey::new_unique());

        let mut expected = update_feed(
            &program_id,
            &payer,
            &pda_pubkey,
            FeedUpdateArgs {
                name: Some("Test Feed".to_string()),
                groups: None,
            },
        );
        let (permission_pda_pubkey, _) = get_permission_pda(&program_id, &payer);
        expected
            .accounts
            .push(AccountMeta::new_readonly(permission_pda_pubkey, false));

        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        client
            .expect_get_account()
            .with(predicate::eq(permission_pda_pubkey))
            .returning(move |_| Ok(Account::new(0, 0, &program_id)));

        let res = UpdateFeedCommand {
            pubkey: pda_pubkey,
            name: Some("Test Feed".to_string()),
            groups: None,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
