use crate::{
    doublezerocommand::CliCommand,
    feed::resolve::pubkey_or_code,
    helpers::parse_or_resolve_exchange,
    validators::{validate_code, validate_pubkey, validate_pubkey_or_code},
};
use clap::{ArgGroup, Args};
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::{activate::ActivateFeedCommand, get::GetFeedCommand};
use doublezero_serviceability::pda::get_stake_mirror_pda;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

/// Admit a feed that was waiting on a conformance verdict.
///
/// Signed by a `FEED_AUTHORITY` or `FOUNDATION` key. The feed's own builder cannot: it may halt and
/// resume its feed, but a builder that could admit it would be attesting to its own conformance.
#[derive(Args, Debug)]
#[clap(group(ArgGroup::new("target").args(&["pubkey", "code"]).required(true)))]
pub struct ActivateFeedCliCommand {
    /// Feed pubkey to activate
    #[arg(long, value_parser = validate_pubkey, conflicts_with = "exchange")]
    pub pubkey: Option<String>,
    /// Feed code to activate, which names one feed only together with its metro
    #[arg(long, value_parser = validate_code, requires = "exchange")]
    pub code: Option<String>,
    /// Metro (exchange) pubkey or code carrying the feed named by --code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub exchange: Option<String>,
}

impl ActivateFeedCliCommand {
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

        let exchange = self
            .exchange
            .as_deref()
            .map(|e| parse_or_resolve_exchange(client, e))
            .transpose()?;
        let (pubkey, feed) = client.get_feed(GetFeedCommand {
            pubkey_or_code: pubkey_or_code(self.pubkey, self.code)?,
            exchange,
        })?;

        // Derived from the feed's own `stake_ref` rather than asked for. The feed already records
        // which stake backs it, so a flag would only be a way to name a different one.
        let stake_mirror = (feed.builder != Pubkey::default())
            .then(|| get_stake_mirror_pda(&client.get_program_id(), &feed.stake_ref).0);

        let signature = client.activate_feed(ActivateFeedCommand {
            pubkey,
            stake_mirror,
        })?;
        print_signature(out, &signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand, feed::activate::ActivateFeedCliCommand,
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::feed::{activate::ActivateFeedCommand, get::GetFeedCommand},
        AccountType, Feed,
    };
    use doublezero_serviceability::{pda::get_stake_mirror_pda, state::feed::FeedStatus};
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    fn pending_feed(builder: Pubkey, stake_ref: Pubkey) -> Feed {
        Feed {
            account_type: AccountType::Feed,
            owner: builder,
            bump_seed: 255,
            code: "tokyo1".to_string(),
            name: "Tokyo".to_string(),
            exchange: Pubkey::new_unique(),
            groups: vec![],
            builder,
            stake_ref,
            status: FeedStatus::Pending,
            ..Default::default()
        }
    }

    /// A staked feed's mirror is derived from the feed rather than asked for, so a caller cannot
    /// name a different stake than the one backing it.
    #[test]
    fn test_cli_feed_activate_derives_the_mirror_from_the_feed() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let feed_pk = Pubkey::new_unique();
        let stake_ref = Pubkey::new_unique();
        let feed = pending_feed(Pubkey::new_unique(), stake_ref);
        let signature = Signature::new_unique();
        let program_id = client.get_program_id();
        let (expected_mirror, _) = get_stake_mirror_pda(&program_id, &stake_ref);

        client
            .expect_get_feed()
            .with(predicate::eq(GetFeedCommand {
                pubkey_or_code: feed_pk.to_string(),
                exchange: None,
            }))
            .times(1)
            .returning(move |_| Ok((feed_pk, feed.clone())));
        client
            .expect_activate_feed()
            .with(predicate::eq(ActivateFeedCommand {
                pubkey: feed_pk,
                stake_mirror: Some(expected_mirror),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ActivateFeedCliCommand {
                pubkey: Some(feed_pk.to_string()),
                code: None,
                exchange: None,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert!(String::from_utf8(output)
            .unwrap()
            .contains(&format!("Signature: {signature}")));
    }

    /// A catalog feed has no stake to re-read, so it sends no mirror. Sending one would ask the
    /// program for an account it refuses.
    #[test]
    fn test_cli_feed_activate_sends_no_mirror_for_a_catalog_feed() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let feed_pk = Pubkey::new_unique();
        let mut feed = pending_feed(Pubkey::default(), Pubkey::default());
        feed.owner = Pubkey::new_unique();
        let signature = Signature::new_unique();

        client
            .expect_get_feed()
            .times(1)
            .returning(move |_| Ok((feed_pk, feed.clone())));
        client
            .expect_activate_feed()
            .with(predicate::eq(ActivateFeedCommand {
                pubkey: feed_pk,
                stake_mirror: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ActivateFeedCliCommand {
                pubkey: Some(feed_pk.to_string()),
                code: None,
                exchange: None,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
    }
}
