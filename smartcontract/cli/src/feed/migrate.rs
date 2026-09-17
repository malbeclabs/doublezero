// TODO: Remove after migration is complete

use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::{
    commands::feed::{
        get::GetFeedCommand,
        list::ListFeedCommand,
        migrate::{MigrateFeedCommand, MAX_FEEDS_PER_TRANSACTION},
    },
    utils::parse_pubkey,
    Feed, FeedChain,
};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, io::Write};

#[derive(Args, Debug)]
pub struct MigrateFeedCliCommand {
    /// Chain to write on each feed. `unspecified` is refused.
    #[arg(long)]
    pub chain: FeedChain,
    /// Feed pubkey or name (repeatable). A name that matches more than one feed fails.
    #[arg(long = "feed", required = true, num_args = 1..)]
    pub feeds: Vec<String>,
}

impl MigrateFeedCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        require!(
            client,
            RequirementCheck::KEYPAIR | RequirementCheck::BALANCE
        );

        if self.chain == FeedChain::Unspecified {
            eyre::bail!("--chain unspecified is not a migration; pass solana or hyperliquid");
        }

        let mut listed = None;
        let mut pending = Vec::new();
        for arg in &self.feeds {
            let (pubkey, feed) = resolve_feed(client, arg, &mut listed)?;
            if pending.iter().any(|(pk, _)| pk == &pubkey) {
                continue;
            }
            if feed.feed_chain != FeedChain::Unspecified {
                writeln!(out, "skipping {}: already {}", feed.name, feed.feed_chain)?;
                continue;
            }
            pending.push((pubkey, feed));
        }

        for chunk in pending.chunks(MAX_FEEDS_PER_TRANSACTION) {
            let signature = client.migrate_feed(MigrateFeedCommand {
                pubkeys: chunk.iter().map(|(pk, _)| *pk).collect(),
                feed_chain: self.chain,
            })?;
            print_signature(out, &signature)?;
        }

        Ok(())
    }
}

fn resolve_feed<C: CliCommand>(
    client: &C,
    arg: &str,
    listed: &mut Option<HashMap<Pubkey, Feed>>,
) -> eyre::Result<(Pubkey, Feed)> {
    if parse_pubkey(arg).is_some() {
        return client.get_feed(GetFeedCommand {
            pubkey_or_code: arg.to_string(),
            exchange: None,
        });
    }

    if listed.is_none() {
        *listed = Some(client.list_feed(ListFeedCommand)?);
    }
    let matches: Vec<(Pubkey, Feed)> = listed
        .as_ref()
        .unwrap()
        .iter()
        .filter(|(_, feed)| feed.name == arg)
        .map(|(pk, feed)| (*pk, feed.clone()))
        .collect();

    match matches.len() {
        0 => eyre::bail!("feed named {arg} not found"),
        1 => Ok(matches.into_iter().next().unwrap()),
        n => eyre::bail!(
            "feed name {arg} matches {n} feeds; pass a pubkey from `doublezero feed list`"
        ),
    }
}

#[cfg(test)]
mod tests {
    use super::MigrateFeedCliCommand;
    use crate::tests::utils::create_test_client;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::feed::{get::GetFeedCommand, list::ListFeedCommand, migrate::MigrateFeedCommand},
        AccountType, Feed, FeedChain,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::collections::HashMap;

    fn test_feed(name: &str, chain: FeedChain) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 0,
            code: "shreds".to_string(),
            name: name.to_string(),
            exchange: Pubkey::new_unique(),
            groups: vec![],
            feed_chain: chain,
            ..Default::default()
        }
    }

    #[test]
    fn test_cli_feed_migrate_sends_the_named_feed() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let feed_pk = Pubkey::new_unique();
        let feed = test_feed("Shreds NY", FeedChain::Unspecified);
        let signature = Signature::new_unique();

        client
            .expect_list_feed()
            .with(predicate::eq(ListFeedCommand))
            .times(1)
            .returning(move |_| Ok(HashMap::from([(feed_pk, feed.clone())])));
        client
            .expect_migrate_feed()
            .with(predicate::eq(MigrateFeedCommand {
                pubkeys: vec![feed_pk],
                feed_chain: FeedChain::Solana,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            MigrateFeedCliCommand {
                chain: FeedChain::Solana,
                feeds: vec!["Shreds NY".to_string()],
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert_eq!(
            String::from_utf8(output).unwrap(),
            format!("Signature: {signature}\n")
        );
    }

    #[test]
    fn test_cli_feed_migrate_sends_the_pubkey() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let feed_pk = Pubkey::new_unique();
        let feed = test_feed("Shreds NY", FeedChain::Unspecified);
        let signature = Signature::new_unique();

        client
            .expect_get_feed()
            .with(predicate::eq(GetFeedCommand {
                pubkey_or_code: feed_pk.to_string(),
                exchange: None,
            }))
            .times(1)
            .returning(move |_| Ok((feed_pk, feed.clone())));
        client
            .expect_migrate_feed()
            .with(predicate::eq(MigrateFeedCommand {
                pubkeys: vec![feed_pk],
                feed_chain: FeedChain::Hyperliquid,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            MigrateFeedCliCommand {
                chain: FeedChain::Hyperliquid,
                feeds: vec![feed_pk.to_string()],
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert_eq!(
            String::from_utf8(output).unwrap(),
            format!("Signature: {signature}\n")
        );
    }

    #[test]
    fn test_cli_feed_migrate_skips_a_feed_that_already_has_a_chain() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let feed_pk = Pubkey::new_unique();
        let feed = test_feed("Shreds NY", FeedChain::Solana);

        client
            .expect_list_feed()
            .with(predicate::eq(ListFeedCommand))
            .times(1)
            .returning(move |_| Ok(HashMap::from([(feed_pk, feed.clone())])));
        client.expect_migrate_feed().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            MigrateFeedCliCommand {
                chain: FeedChain::Hyperliquid,
                feeds: vec!["Shreds NY".to_string()],
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert_eq!(
            String::from_utf8(output).unwrap(),
            "skipping Shreds NY: already solana\n"
        );
    }

    #[test]
    fn test_cli_feed_migrate_refuses_an_ambiguous_name() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let one = test_feed("Shreds", FeedChain::Unspecified);
        let two = test_feed("Shreds", FeedChain::Unspecified);

        client
            .expect_list_feed()
            .with(predicate::eq(ListFeedCommand))
            .times(1)
            .returning(move |_| {
                Ok(HashMap::from([
                    (Pubkey::new_unique(), one.clone()),
                    (Pubkey::new_unique(), two.clone()),
                ]))
            });
        client.expect_migrate_feed().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            MigrateFeedCliCommand {
                chain: FeedChain::Solana,
                feeds: vec!["Shreds".to_string()],
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("matches 2 feeds"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn test_cli_feed_migrate_refuses_unspecified() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));
        client.expect_migrate_feed().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            MigrateFeedCliCommand {
                chain: FeedChain::Unspecified,
                feeds: vec!["Shreds NY".to_string()],
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("unspecified"),
            "unexpected error: {err}"
        );
    }
}
