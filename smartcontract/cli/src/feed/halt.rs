use crate::{doublezerocommand::CliCommand, feed::resolve::FeedTargetArgs};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::halt::HaltFeedCommand;
use std::io::Write;

/// Stop a feed publishing. Its own builder may sign this, or an operator.
#[derive(Args, Debug)]
pub struct HaltFeedCliCommand {
    #[command(flatten)]
    pub target: FeedTargetArgs,
}

impl HaltFeedCliCommand {
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

        let (pubkey, _feed) = self.target.resolve(client)?;

        let signature = client.halt_feed(HaltFeedCommand { pubkey })?;
        print_signature(out, &signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        feed::{halt::HaltFeedCliCommand, resolve::FeedTargetArgs},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::feed::{get::GetFeedCommand, halt::HaltFeedCommand},
        AccountType, Feed,
    };
    use doublezero_serviceability::state::feed::FeedStatus;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    /// Halting names the feed and nothing else: there is no stake to re-read, so the mirror a
    /// staked feed's activate and resume carry has no place here.
    #[test]
    fn test_cli_feed_halt_sends_the_resolved_feed() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let feed_pk = Pubkey::new_unique();
        let builder = Pubkey::new_unique();
        let feed = Feed {
            account_type: AccountType::Feed,
            owner: builder,
            bump_seed: 255,
            code: "tokyo1".to_string(),
            name: "Tokyo".to_string(),
            exchange: Pubkey::new_unique(),
            groups: vec![],
            builder,
            stake_ref: Pubkey::new_unique(),
            status: FeedStatus::Active,
            ..Default::default()
        };
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
            .expect_halt_feed()
            .with(predicate::eq(HaltFeedCommand { pubkey: feed_pk }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            HaltFeedCliCommand {
                target: FeedTargetArgs {
                    pubkey: Some(feed_pk.to_string()),
                    code: None,
                    exchange: None,
                },
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert_eq!(
            String::from_utf8(output).unwrap(),
            format!("Signature: {signature}\n")
        );
    }
}
