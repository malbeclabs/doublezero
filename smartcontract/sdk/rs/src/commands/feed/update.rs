use std::collections::HashSet;

use crate::{AccountData, DoubleZeroClient};
use doublezero_serviceability::{
    processors::feed::{create::MAX_FEED_GROUPS, update::FeedUpdateArgs},
    state::feed::FeedChain,
};
use doublezero_serviceability_instruction::feed::update_feed;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

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
        // The program refuses a new list with a repeated group or more than 20 groups. It sees
        // only the groups that change, so the command refuses a group repeated in the request.
        if let Some(groups) = &self.groups {
            if let Some(group) = groups
                .iter()
                .enumerate()
                .find_map(|(i, group)| groups[..i].contains(group).then_some(group))
            {
                eyre::bail!("group {group} is named twice");
            }
            if groups.len() > MAX_FEED_GROUPS {
                eyre::bail!(
                    "{} groups exceed the {MAX_FEED_GROUPS}-group limit of a feed",
                    groups.len()
                );
            }
        }

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let args = FeedUpdateArgs {
            name: self.name.clone(),
            feed_chain: self.feed_chain,
            ..Default::default()
        };

        let mut instructions = Vec::with_capacity(pubkeys.len());
        for pubkey in &pubkeys {
            // The program takes the groups that change, not the whole list, so the command reads
            // the feed's groups now. A rename or a chain change costs no read. A swap of 20 groups
            // for 20 others needs 40 group accounts, which do not fit in one transaction. Make
            // that swap in two updates.
            let (read_groups, added, removed) = match &self.groups {
                Some(groups) => {
                    let AccountData::Feed(feed) = client.get(*pubkey)? else {
                        eyre::bail!("{pubkey} is not a feed");
                    };
                    (
                        feed.groups.clone(),
                        groups
                            .iter()
                            .filter(|group| !feed.groups.contains(group))
                            .copied()
                            .collect(),
                        feed.groups
                            .iter()
                            .filter(|group| !groups.contains(group))
                            .copied()
                            .collect(),
                    )
                }
                None => (Vec::new(), Vec::new(), Vec::new()),
            };
            instructions.push(update_feed(
                &program_id,
                &payer,
                pubkey,
                &read_groups,
                &added,
                &removed,
                args.clone(),
            ));
        }
        client.send_instructions(instructions)
    }
}

#[cfg(test)]
mod tests {
    use super::{UpdateFeedCommand, MAX_FEEDS_PER_TRANSACTION};
    use crate::{tests::utils::create_test_client, AccountData, DoubleZeroClient};
    use doublezero_serviceability::{
        pda::get_feed_pda,
        processors::feed::update::FeedUpdateArgs,
        state::{
            accounttype::AccountType,
            feed::{Feed, FeedChain},
        },
    };
    use doublezero_serviceability_instruction::{compute_budget_prelude, feed::update_feed};
    use mockall::predicate;
    use solana_sdk::{
        instruction::AccountMeta, message::Message, pubkey::Pubkey, signature::Signature,
    };

    fn chain_args() -> FeedUpdateArgs {
        FeedUpdateArgs {
            feed_chain: Some(FeedChain::Solana),
            ..Default::default()
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
            &[],
            &[],
            &[],
            FeedUpdateArgs {
                name: Some("Test Feed".to_string()),
                ..Default::default()
            },
        );
        client
            .expect_send_instructions()
            .with(predicate::eq(vec![expected]))
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateFeedCommand {
            pubkeys: vec![pda_pubkey],
            name: Some("Test Feed".to_string()),
            groups: None,
            feed_chain: None,
        }
        .execute(&client);
        assert!(res.is_ok());
    }

    // A group change sends only the groups that change, read against each feed's groups now.
    #[test]
    fn test_commands_feed_update_sends_the_added_and_removed_groups() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (kept, added, removed) = (
            Pubkey::new_unique(),
            Pubkey::new_unique(),
            Pubkey::new_unique(),
        );
        let feed_pk = Pubkey::new_unique();
        client
            .expect_get()
            .with(predicate::eq(feed_pk))
            .returning(move |_| {
                Ok(AccountData::Feed(Feed {
                    account_type: AccountType::Feed,
                    groups: vec![kept, removed],
                    ..Default::default()
                }))
            });
        let expected = vec![update_feed(
            &program_id,
            &payer,
            &feed_pk,
            &[kept, removed],
            &[added],
            &[removed],
            FeedUpdateArgs::default(),
        )];
        client
            .expect_send_instructions()
            .with(predicate::eq(expected))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateFeedCommand {
            pubkeys: vec![feed_pk],
            name: None,
            groups: Some(vec![kept, added]),
            feed_chain: None,
        }
        .execute(&client);
        assert!(res.is_ok(), "{res:?}");
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
            update_feed(&program_id, &payer, &feed_a, &[], &[], &[], args.clone()),
            update_feed(&program_id, &payer, &feed_b, &[], &[], &[], args),
        ];
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

        client
            .expect_send_instructions()
            .with(predicate::eq(vec![update_feed(
                &program_id,
                &payer,
                &feed_pk,
                &[],
                &[],
                &[],
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
    fn test_commands_feed_update_refuses_a_group_named_twice() {
        let client = create_test_client();
        let group = Pubkey::new_unique();
        let err = UpdateFeedCommand {
            pubkeys: vec![Pubkey::new_unique()],
            name: None,
            groups: Some(vec![group, group, Pubkey::new_unique()]),
            feed_chain: None,
        }
        .execute(&client)
        .unwrap_err();
        assert_eq!(err.to_string(), format!("group {group} is named twice"));
    }

    #[test]
    fn test_commands_feed_update_refuses_more_than_twenty_groups() {
        let client = create_test_client();
        let err = UpdateFeedCommand {
            pubkeys: vec![Pubkey::new_unique()],
            name: None,
            groups: Some((0..21).map(|_| Pubkey::new_unique()).collect()),
            feed_chain: None,
        }
        .execute(&client)
        .unwrap_err();
        assert_eq!(
            err.to_string(),
            "21 groups exceed the 20-group limit of a feed"
        );
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
            pubkeys: (0..MAX_FEEDS_PER_TRANSACTION + 1)
                .map(|_| Pubkey::new_unique())
                .collect(),
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
            let mut ix = update_feed(
                &program_id,
                &payer,
                &Pubkey::new_unique(),
                &[],
                &[],
                &[],
                chain_args(),
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
