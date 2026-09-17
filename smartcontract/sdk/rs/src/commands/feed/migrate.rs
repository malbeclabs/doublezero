use crate::{commands::common::append_payer_permission_account, DoubleZeroClient};
use doublezero_serviceability::{
    processors::feed::migrate::FeedMigrateArgs, state::feed::FeedChain,
};
use doublezero_serviceability_instruction::feed::migrate_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

pub const MAX_FEEDS_PER_TRANSACTION: usize = 8;

#[derive(Debug, PartialEq, Clone)]
pub struct MigrateFeedCommand {
    pub pubkeys: Vec<Pubkey>,
    pub feed_chain: FeedChain,
}

impl MigrateFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        if self.feed_chain == FeedChain::Unspecified {
            eyre::bail!("unspecified is not a chain to migrate to; pass solana or hyperliquid");
        }
        if self.pubkeys.is_empty() {
            eyre::bail!("at least one feed is required");
        }
        if self.pubkeys.len() > MAX_FEEDS_PER_TRANSACTION {
            eyre::bail!(
                "{} feeds exceed the {MAX_FEEDS_PER_TRANSACTION}-feed transaction limit; send one transaction per chunk",
                self.pubkeys.len()
            );
        }

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let mut instructions = Vec::with_capacity(self.pubkeys.len());
        for pubkey in &self.pubkeys {
            let mut ix = migrate_feed(
                &program_id,
                &payer,
                pubkey,
                FeedMigrateArgs {
                    feed_chain: self.feed_chain,
                },
            );
            append_payer_permission_account(client, &mut ix)?;
            instructions.push(ix);
        }
        client.send_instructions(instructions)
    }
}

#[cfg(test)]
mod tests {
    use super::{MigrateFeedCommand, MAX_FEEDS_PER_TRANSACTION};
    use crate::{
        tests::utils::{create_test_client, expect_missing_permission_account},
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_permission_pda, processors::feed::migrate::FeedMigrateArgs, state::feed::FeedChain,
    };
    use doublezero_serviceability_instruction::{compute_budget_prelude, feed::migrate_feed};
    use mockall::predicate;
    use solana_sdk::{
        account::Account, instruction::AccountMeta, message::Message, pubkey::Pubkey,
        signature::Signature,
    };

    #[test]
    fn test_commands_feed_migrate_sends_one_instruction_per_feed() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let feed_a = Pubkey::new_unique();
        let feed_b = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let expected: Vec<_> = [feed_a, feed_b]
            .into_iter()
            .map(|pubkey| {
                migrate_feed(
                    &program_id,
                    &payer,
                    &pubkey,
                    FeedMigrateArgs {
                        feed_chain: FeedChain::Solana,
                    },
                )
            })
            .collect();

        expect_missing_permission_account(&mut client);
        client
            .expect_send_instructions()
            .with(predicate::eq(expected))
            .times(1)
            .returning(move |_| Ok(signature));

        let res = MigrateFeedCommand {
            pubkeys: vec![feed_a, feed_b],
            feed_chain: FeedChain::Solana,
        }
        .execute(&client);
        assert_eq!(res.unwrap(), signature);
    }

    #[test]
    fn test_commands_feed_migrate_appends_the_payer_permission_account() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let mut expected = migrate_feed(
            &program_id,
            &payer,
            &feed_pk,
            FeedMigrateArgs {
                feed_chain: FeedChain::Hyperliquid,
            },
        );
        let (permission_pda, _) = get_permission_pda(&program_id, &payer);
        expected
            .accounts
            .push(AccountMeta::new_readonly(permission_pda, false));

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![permission_pda]))
            .returning(move |_| Ok(vec![Some(Account::new(0, 0, &program_id))]));
        client
            .expect_send_instructions()
            .with(predicate::eq(vec![expected]))
            .times(1)
            .returning(move |_| Ok(signature));

        let res = MigrateFeedCommand {
            pubkeys: vec![feed_pk],
            feed_chain: FeedChain::Hyperliquid,
        }
        .execute(&client);
        assert_eq!(res.unwrap(), signature);
    }

    #[test]
    fn test_commands_feed_migrate_refuses_unspecified() {
        let client = create_test_client();
        let err = MigrateFeedCommand {
            pubkeys: vec![Pubkey::new_unique()],
            feed_chain: FeedChain::Unspecified,
        }
        .execute(&client)
        .unwrap_err();
        assert!(
            err.to_string().contains("unspecified"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn max_feed_batch_fits_transaction_size() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let permission = Pubkey::new_unique();
        let mut instructions = compute_budget_prelude().to_vec();
        for _ in 0..MAX_FEEDS_PER_TRANSACTION {
            let mut ix = migrate_feed(
                &program_id,
                &payer,
                &Pubkey::new_unique(),
                FeedMigrateArgs {
                    feed_chain: FeedChain::Solana,
                },
            );
            ix.accounts
                .push(AccountMeta::new_readonly(permission, false));
            instructions.push(ix);
        }

        let message = Message::new(&instructions, Some(&payer));
        let tx_size =
            1 + 64 * message.header.num_required_signatures as usize + message.serialize().len();
        assert!(
            tx_size <= 1232,
            "max batch transaction is {tx_size} bytes, over the 1232-byte limit"
        );
    }
}
