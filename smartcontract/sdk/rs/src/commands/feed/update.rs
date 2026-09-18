use std::collections::HashSet;

use crate::{commands::common::append_payer_permission_account, DoubleZeroClient};
use doublezero_serviceability::{processors::feed::update::FeedUpdateArgs, state::feed::FeedChain};
use doublezero_serviceability_instruction::feed::update_feed;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

pub const MAX_FEEDS_PER_TRANSACTION: usize = 8;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateFeedCommand {
    pub pubkeys: Vec<Pubkey>,
    pub name: Option<String>,
    /// Replacement multicast group set. `None` leaves the groups unchanged.
    pub groups: Option<Vec<Pubkey>>,
    pub feed_chain: Option<FeedChain>,
}

impl UpdateFeedCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let mut seen = HashSet::new();
        let pubkeys: Vec<Pubkey> = self
            .pubkeys
            .iter()
            .copied()
            .filter(|pubkey| seen.insert(*pubkey))
            .collect();
        if pubkeys.is_empty() {
            eyre::bail!("at least one feed is required");
        }
        if pubkeys.len() > MAX_FEEDS_PER_TRANSACTION {
            eyre::bail!(
                "{} feeds exceed the {MAX_FEEDS_PER_TRANSACTION}-feed transaction limit; send one transaction per chunk",
                pubkeys.len()
            );
        }

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let args = FeedUpdateArgs {
            name: self.name.clone(),
            groups: self.groups.clone(),
            feed_chain: self.feed_chain,
        };

        let mut first = update_feed(&program_id, &payer, &pubkeys[0], args.clone());
        let account_count = first.accounts.len();
        append_payer_permission_account(client, &mut first)?;
        let permission_meta: Option<AccountMeta> = (first.accounts.len() > account_count)
            .then(|| first.accounts.last().cloned())
            .flatten();

        let mut instructions = Vec::with_capacity(pubkeys.len());
        instructions.push(first);
        for pubkey in &pubkeys[1..] {
            let mut ix = update_feed(&program_id, &payer, pubkey, args.clone());
            if let Some(ref meta) = permission_meta {
                ix.accounts.push(meta.clone());
            }
            instructions.push(ix);
        }
        client.send_instructions(instructions)
    }
}

#[cfg(test)]
mod tests {
    use super::{UpdateFeedCommand, MAX_FEEDS_PER_TRANSACTION};
    use crate::{
        tests::utils::{create_test_client, expect_missing_permission_account},
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::{get_feed_pda, get_permission_pda},
        processors::feed::update::FeedUpdateArgs,
        state::feed::FeedChain,
    };
    use doublezero_serviceability_instruction::{compute_budget_prelude, feed::update_feed};
    use mockall::predicate;
    use solana_sdk::{
        account::Account, instruction::AccountMeta, message::Message, pubkey::Pubkey,
        signature::Signature,
    };

    fn chain_args() -> FeedUpdateArgs {
        FeedUpdateArgs {
            name: None,
            groups: None,
            feed_chain: Some(FeedChain::Solana),
        }
    }

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
                feed_chain: None,
            },
        );
        client
            .expect_send_instructions()
            .with(predicate::eq(vec![expected]))
            .returning(|_| Ok(Signature::new_unique()));

        expect_missing_permission_account(&mut client);

        let res = UpdateFeedCommand {
            pubkeys: vec![pda_pubkey],
            name: Some("Test Feed".to_string()),
            groups: None,
            feed_chain: None,
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
                feed_chain: None,
            },
        );
        let (permission_pda_pubkey, _) = get_permission_pda(&program_id, &payer);
        expected
            .accounts
            .push(AccountMeta::new_readonly(permission_pda_pubkey, false));

        client
            .expect_send_instructions()
            .with(predicate::eq(vec![expected]))
            .returning(|_| Ok(Signature::new_unique()));

        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![permission_pda_pubkey]))
            .returning(move |_| Ok(vec![Some(Account::new(0, 0, &program_id))]));

        let res = UpdateFeedCommand {
            pubkeys: vec![pda_pubkey],
            name: Some("Test Feed".to_string()),
            groups: None,
            feed_chain: None,
        }
        .execute(&client);
        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_feed_update_sends_one_instruction_per_feed() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let feed_a = Pubkey::new_unique();
        let feed_b = Pubkey::new_unique();
        let signature = Signature::new_unique();
        let args = chain_args();

        let expected = vec![
            update_feed(&program_id, &payer, &feed_a, args.clone()),
            update_feed(&program_id, &payer, &feed_b, args),
        ];
        expect_missing_permission_account(&mut client);
        client
            .expect_send_instructions()
            .with(predicate::eq(expected))
            .times(1)
            .returning(move |_| Ok(signature));

        let res = UpdateFeedCommand {
            pubkeys: vec![feed_a, feed_b],
            name: None,
            groups: None,
            feed_chain: Some(FeedChain::Solana),
        }
        .execute(&client);
        assert_eq!(res.unwrap(), signature);
    }

    #[test]
    fn test_commands_feed_update_dedups_a_repeated_pubkey() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        expect_missing_permission_account(&mut client);
        client
            .expect_send_instructions()
            .with(predicate::eq(vec![update_feed(
                &program_id,
                &payer,
                &feed_pk,
                chain_args(),
            )]))
            .times(1)
            .returning(move |_| Ok(signature));

        let res = UpdateFeedCommand {
            pubkeys: vec![feed_pk, feed_pk],
            name: None,
            groups: None,
            feed_chain: Some(FeedChain::Solana),
        }
        .execute(&client);
        assert_eq!(res.unwrap(), signature);
    }

    #[test]
    fn test_commands_feed_update_refuses_an_empty_list() {
        let client = create_test_client();
        let err = UpdateFeedCommand {
            pubkeys: vec![],
            name: None,
            groups: None,
            feed_chain: Some(FeedChain::Solana),
        }
        .execute(&client)
        .unwrap_err();
        assert_eq!(err.to_string(), "at least one feed is required");
    }

    #[test]
    fn test_commands_feed_update_refuses_a_ninth_feed() {
        let client = create_test_client();
        let err = UpdateFeedCommand {
            pubkeys: vec![Pubkey::new_unique(); MAX_FEEDS_PER_TRANSACTION + 1],
            name: None,
            groups: None,
            feed_chain: Some(FeedChain::Solana),
        }
        .execute(&client)
        .unwrap_err();
        assert_eq!(
            err.to_string(),
            format!(
                "{} feeds exceed the {MAX_FEEDS_PER_TRANSACTION}-feed transaction limit; send one transaction per chunk",
                MAX_FEEDS_PER_TRANSACTION + 1
            )
        );
    }

    #[test]
    fn max_feed_batch_fits_transaction_size() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let permission = Pubkey::new_unique();
        let mut instructions = compute_budget_prelude().to_vec();
        for _ in 0..MAX_FEEDS_PER_TRANSACTION {
            let mut ix = update_feed(&program_id, &payer, &Pubkey::new_unique(), chain_args());
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
