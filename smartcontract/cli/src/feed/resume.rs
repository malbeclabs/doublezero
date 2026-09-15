use crate::{
    doublezerocommand::CliCommand,
    feed::resolve::{stake_mirror_of, FeedTargetArgs},
};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::resume::ResumeFeedCommand;
use std::io::Write;

/// Put a halted feed back to publishing. An operator's halt takes an operator to lift.
#[derive(Args, Debug)]
pub struct ResumeFeedCliCommand {
    #[command(flatten)]
    pub target: FeedTargetArgs,
}

impl ResumeFeedCliCommand {
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

        let (pubkey, feed) = self.target.resolve(client)?;

        let stake_mirror = stake_mirror_of(client, &feed);

        let signature = client.resume_feed(ResumeFeedCommand {
            pubkey,
            stake_mirror,
        })?;
        print_signature(out, &signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        feed::{resolve::FeedTargetArgs, resume::ResumeFeedCliCommand},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::feed::{get::GetFeedCommand, resume::ResumeFeedCommand},
        AccountType, Feed,
    };
    use doublezero_serviceability::{pda::get_stake_mirror_pda, state::feed::FeedStatus};
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    fn halted_feed(builder: Pubkey, stake_ref: Pubkey) -> Feed {
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
            status: FeedStatus::Halted,
            ..Default::default()
        }
    }

    /// Resuming re-proves the stake still covers the rate, so a staked feed sends its mirror, and
    /// the address comes from the feed rather than from a flag.
    #[test]
    fn test_cli_feed_resume_derives_the_mirror_from_the_feed() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let feed_pk = Pubkey::new_unique();
        let stake_ref = Pubkey::new_unique();
        let feed = halted_feed(Pubkey::new_unique(), stake_ref);
        let signature = Signature::new_unique();
        let (expected_mirror, _) = get_stake_mirror_pda(&client.get_program_id(), &stake_ref);

        client
            .expect_get_feed()
            .with(predicate::eq(GetFeedCommand {
                pubkey_or_code: feed_pk.to_string(),
                exchange: None,
            }))
            .times(1)
            .returning(move |_| Ok((feed_pk, feed.clone())));
        client
            .expect_resume_feed()
            .with(predicate::eq(ResumeFeedCommand {
                pubkey: feed_pk,
                stake_mirror: Some(expected_mirror),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ResumeFeedCliCommand {
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

    /// A catalog feed has no stake to re-prove, so it sends no mirror.
    #[test]
    fn test_cli_feed_resume_sends_no_mirror_for_a_catalog_feed() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let feed_pk = Pubkey::new_unique();
        let feed = halted_feed(Pubkey::default(), Pubkey::default());
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
            .expect_resume_feed()
            .with(predicate::eq(ResumeFeedCommand {
                pubkey: feed_pk,
                stake_mirror: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ResumeFeedCliCommand {
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
