use crate::{doublezerocommand::CliCommand, feed::resolve::FeedTargetArgs};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::finalize_retirement::FinalizeFeedRetirementCommand;
use std::io::Write;

/// End a retirement once its notice has elapsed.
///
/// Permissionless: the clock decides, not a key. Anyone may call it, and it refuses until the
/// notice the feed's seat holders were promised has run out.
#[derive(Args, Debug)]
pub struct FinalizeFeedRetirementCliCommand {
    #[command(flatten)]
    pub target: FeedTargetArgs,
}

impl FinalizeFeedRetirementCliCommand {
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

        let signature =
            client.finalize_feed_retirement(FinalizeFeedRetirementCommand { pubkey })?;
        print_signature(out, &signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        feed::{finalize_retirement::FinalizeFeedRetirementCliCommand, resolve::FeedTargetArgs},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::feed::{finalize_retirement::FinalizeFeedRetirementCommand, get::GetFeedCommand},
        AccountType, Feed,
    };
    use doublezero_serviceability::state::feed::FeedStatus;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    /// Finalizing names the feed and nothing else. It is permissionless, so the command carries
    /// nothing an authority would be checked against.
    #[test]
    fn test_cli_feed_finalize_retirement_sends_the_resolved_feed() {
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
            status: FeedStatus::Retiring,
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
            .expect_finalize_feed_retirement()
            .with(predicate::eq(FinalizeFeedRetirementCommand {
                pubkey: feed_pk,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            FinalizeFeedRetirementCliCommand {
                target: FeedTargetArgs {
                    pubkey: vec![feed_pk.to_string()],
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
