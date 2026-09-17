use crate::{
    doublezerocommand::CliCommand,
    helpers::{resolve_exchange_pk, resolve_multicastgroup_pk},
    validators::{
        validate_code, validate_parse_bandwidth, validate_pubkey, validate_pubkey_or_code,
    },
};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::create::{CreateFeedCommand, FeedStakeTerms};
use eyre::WrapErr;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

/// The committed rate, as a bandwidth or as the unmetered tier.
///
/// `unmetered` rather than a number, because the tier is `u64::MAX` and nobody should have to type
/// that. Anything else is a bandwidth and goes through the shared parser, so this flag reads the
/// way every other bandwidth flag in the CLI does.
fn validate_parse_committed_rate(val: &str) -> Result<u64, String> {
    if val.eq_ignore_ascii_case("unmetered") {
        return Ok(u64::MAX);
    }
    validate_parse_bandwidth(val)
}

/// SHA-256 of the declared service level, as 64 hex characters.
fn validate_parse_sla_hash(val: &str) -> Result<String, String> {
    let trimmed = val.strip_prefix("0x").unwrap_or(val);
    if trimmed.len() != 64 || !trimmed.chars().all(|c| c.is_ascii_hexdigit()) {
        return Err(String::from(
            "SLA hash must be 64 hex characters (a SHA-256 digest)",
        ));
    }
    Ok(trimmed.to_string())
}

fn parse_sla_hash(val: &str) -> [u8; 32] {
    let mut hash = [0u8; 32];
    for (index, byte) in hash.iter_mut().enumerate() {
        // The validator already rejected anything that is not 64 hex characters, so this cannot
        // fail on a value clap accepted.
        *byte = u8::from_str_radix(&val[index * 2..index * 2 + 2], 16).unwrap_or_default();
    }
    hash
}

#[derive(Args, Debug, Default)]
pub struct CreateFeedCliCommand {
    /// Unique code for the feed (immutable; part of the PDA seed)
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Human-readable name for the feed
    #[arg(long)]
    pub name: String,
    /// Metro (exchange) pubkey or code this feed serves (immutable; part of the PDA seed)
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub exchange: String,
    /// Multicast group pubkey or code joinable in this metro (repeatable)
    #[arg(long = "group", value_parser = validate_pubkey_or_code, num_args = 1..)]
    pub groups: Vec<String>,

    // The RFC-28 terms. All five or none: a feed carrying some of them is not something the
    // program can make, so clap refuses the partial set rather than the ledger refusing the
    // transaction.
    /// The builder deploying this feed. Makes it a staked feed, which starts Pending
    #[arg(long, value_parser = validate_pubkey,
          requires_all = ["stake_ref", "spec_id", "sla_hash", "committed_rate"])]
    pub builder: Option<String>,
    /// The BuilderStake account on Solana holding this feed's bond
    #[arg(long, value_parser = validate_pubkey, requires = "builder")]
    pub stake_ref: Option<String>,
    /// The wire format this feed publishes, as <spec>@<version>
    #[arg(long, requires = "builder")]
    pub spec_id: Option<String>,
    /// SHA-256 of the declared service level, as 64 hex characters
    #[arg(long, value_parser = validate_parse_sla_hash, requires = "builder")]
    pub sla_hash: Option<String>,
    /// Rate the bond must cover, as a bandwidth (1Gbps) or `unmetered`
    #[arg(long, value_parser = validate_parse_committed_rate, requires = "builder")]
    pub committed_rate: Option<u64>,
}

impl CreateFeedCliCommand {
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

        let exchange = resolve_exchange_pk(client, &self.exchange)
            .wrap_err_with(|| format!("Exchange not found: {}", self.exchange))?;
        let groups = self
            .groups
            .iter()
            .map(|g| resolve_multicastgroup_pk(client, g))
            .collect::<eyre::Result<Vec<_>>>()?;

        // clap ties the five together, so the builder alone decides whether this is staked.
        let stake_terms = self
            .builder
            .as_ref()
            .map(|builder| -> eyre::Result<FeedStakeTerms> {
                Ok(FeedStakeTerms {
                    builder: builder.parse::<Pubkey>()?,
                    stake_ref: self
                        .stake_ref
                        .as_ref()
                        .expect("clap requires stake_ref with builder")
                        .parse::<Pubkey>()?,
                    spec_id: self
                        .spec_id
                        .clone()
                        .expect("clap requires spec_id with builder"),
                    sla_hash: parse_sla_hash(
                        self.sla_hash
                            .as_ref()
                            .expect("clap requires sla_hash with builder"),
                    ),
                    committed_rate_bits_per_sec: self
                        .committed_rate
                        .expect("clap requires committed_rate with builder"),
                })
            })
            .transpose()?;

        let (signature, _pubkey) = client.create_feed(CreateFeedCommand {
            code: self.code,
            name: self.name,
            exchange,
            groups,
            stake_terms,
        })?;

        print_signature(out, &signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        feed::create::{
            parse_sla_hash, validate_parse_committed_rate, validate_parse_sla_hash,
            CreateFeedCliCommand,
        },
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::{
            exchange::get::GetExchangeCommand,
            feed::create::{CreateFeedCommand, FeedStakeTerms},
            multicastgroup::get::GetMulticastGroupCommand,
        },
        AccountType, Exchange, ExchangeStatus, MulticastGroup, MulticastGroupStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    fn test_exchange(code: &str) -> Exchange {
        Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: code.to_string(),
            name: "Test Exchange".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 12.34,
            lng: 56.78,
            bgp_community: 1,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::new_unique(),
        }
    }

    fn test_multicastgroup(code: &str) -> MulticastGroup {
        MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 1].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            code: code.to_string(),
            owner: Pubkey::new_unique(),
            publisher_count: 0,
            subscriber_count: 0,
        }
    }

    #[test]
    fn test_cli_feed_create_with_pubkeys() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let exchange_pk = Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
        let group_pk = Pubkey::from_str_const("1119DWteoLSdjvrT6g6L8C2PfDD2faiTQUpsjY2RiF");
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        // A pubkey input is read back too, so a feed cannot name a metro or a group that the ledger
        // does not carry.
        let exchange = test_exchange("xchi");
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: exchange_pk.to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((exchange_pk, exchange.clone())));

        let mgroup = test_multicastgroup("mg01");
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: group_pk.to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((group_pk, mgroup.clone())));

        client
            .expect_create_feed()
            .with(predicate::eq(CreateFeedCommand {
                code: "feed01".to_string(),
                name: "Feed".to_string(),
                exchange: exchange_pk,
                groups: vec![group_pk],
                stake_terms: None,
            }))
            .times(1)
            .returning(move |_| Ok((signature, feed_pk)));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            CreateFeedCliCommand {
                code: "feed01".to_string(),
                name: "Feed".to_string(),
                exchange: exchange_pk.to_string(),
                groups: vec![group_pk.to_string()],
                ..Default::default()
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
    fn test_cli_feed_create_with_codes() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let exchange_pk = Pubkey::new_unique();
        let group_pk = Pubkey::new_unique();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let exchange = test_exchange("xchi");
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: "xchi".to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((exchange_pk, exchange.clone())));

        let mgroup = test_multicastgroup("mg01");
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: "mg01".to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((group_pk, mgroup.clone())));

        client
            .expect_create_feed()
            .with(predicate::eq(CreateFeedCommand {
                code: "feed01".to_string(),
                name: "Feed".to_string(),
                exchange: exchange_pk,
                groups: vec![group_pk],
                stake_terms: None,
            }))
            .times(1)
            .returning(move |_| Ok((signature, feed_pk)));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            CreateFeedCliCommand {
                code: "feed01".to_string(),
                name: "Feed".to_string(),
                exchange: "xchi".to_string(),
                groups: vec!["mg01".to_string()],
                ..Default::default()
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
    fn test_cli_feed_create_unknown_group_code() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let exchange_pk = Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
        let exchange = test_exchange("xchi");
        client
            .expect_get_exchange()
            .returning(move |_| Ok((exchange_pk, exchange.clone())));
        client
            .expect_get_multicastgroup()
            .returning(|_| Err(eyre::eyre!("MulticastGroup with code nope not found")));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            CreateFeedCliCommand {
                code: "feed01".to_string(),
                name: "Feed".to_string(),
                exchange: exchange_pk.to_string(),
                groups: vec!["nope".to_string()],
                ..Default::default()
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("Multicast group not found: nope"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn test_cli_feed_create_unknown_exchange_code() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        client
            .expect_get_exchange()
            .returning(|_| Err(eyre::eyre!("Exchange with code nope not found")));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            CreateFeedCliCommand {
                code: "feed01".to_string(),
                name: "Feed".to_string(),
                exchange: "nope".to_string(),
                groups: vec![],
                ..Default::default()
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("Exchange not found: nope"),
            "unexpected error: {err}"
        );
    }

    #[test]
    fn test_cli_feed_create_staked_passes_the_terms_through() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let exchange_pk = Pubkey::new_unique();
        let group_pk = Pubkey::new_unique();
        let builder_pk = Pubkey::new_unique();
        let stake_pk = Pubkey::new_unique();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let exchange = test_exchange("xtyo");
        client
            .expect_get_exchange()
            .returning(move |_| Ok((exchange_pk, exchange.clone())));
        let mgroup = test_multicastgroup("mg01");
        client
            .expect_get_multicastgroup()
            .returning(move |_| Ok((group_pk, mgroup.clone())));

        client
            .expect_create_feed()
            .with(predicate::eq(CreateFeedCommand {
                code: "tokyo1".to_string(),
                name: "Tokyo".to_string(),
                exchange: exchange_pk,
                groups: vec![group_pk],
                stake_terms: Some(FeedStakeTerms {
                    builder: builder_pk,
                    stake_ref: stake_pk,
                    spec_id: "top-of-book@v1.0.0".to_string(),
                    sla_hash: [9u8; 32],
                    committed_rate_bits_per_sec: 1_000_000_000,
                }),
            }))
            .times(1)
            .returning(move |_| Ok((signature, feed_pk)));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            CreateFeedCliCommand {
                code: "tokyo1".to_string(),
                name: "Tokyo".to_string(),
                exchange: exchange_pk.to_string(),
                groups: vec![group_pk.to_string()],
                builder: Some(builder_pk.to_string()),
                stake_ref: Some(stake_pk.to_string()),
                spec_id: Some("top-of-book@v1.0.0".to_string()),
                sla_hash: Some("09".repeat(32)),
                committed_rate: Some(1_000_000_000),
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
    }

    // The five terms are one value to the program, so a partial set is refused where it is typed
    // rather than by the ledger after a transaction.
    #[test]
    fn test_cli_feed_create_refuses_partial_stake_terms() {
        use clap::Parser;

        #[derive(Parser, Debug)]
        struct TestCli {
            #[command(subcommand)]
            command: TestCommand,
        }

        #[derive(clap::Subcommand, Debug)]
        enum TestCommand {
            Create(CreateFeedCliCommand),
        }

        let builder = Pubkey::new_unique().to_string();
        let stake = Pubkey::new_unique().to_string();
        let base = vec![
            "test".to_string(),
            "create".to_string(),
            "--code".to_string(),
            "tokyo1".to_string(),
            "--name".to_string(),
            "Tokyo".to_string(),
            "--exchange".to_string(),
            "xtyo".to_string(),
        ];

        let mut builder_alone = base.clone();
        builder_alone.extend(["--builder".to_string(), builder.clone()]);
        assert!(
            TestCli::try_parse_from(&builder_alone).is_err(),
            "a builder with no terms must be refused"
        );

        let mut terms_without_builder = base.clone();
        terms_without_builder.extend(["--stake-ref".to_string(), stake.clone()]);
        assert!(
            TestCli::try_parse_from(&terms_without_builder).is_err(),
            "a stake with no builder must be refused"
        );

        let mut all_five = base.clone();
        all_five.extend([
            "--builder".to_string(),
            builder,
            "--stake-ref".to_string(),
            stake,
            "--spec-id".to_string(),
            "top-of-book@v1.0.0".to_string(),
            "--sla-hash".to_string(),
            "09".repeat(32),
            "--committed-rate".to_string(),
            "1Gbps".to_string(),
        ]);
        assert!(
            TestCli::try_parse_from(&all_five).is_ok(),
            "all five together must parse"
        );

        // A catalog feed names none of them and is unaffected.
        assert!(TestCli::try_parse_from(&base).is_ok());
    }

    #[test]
    fn test_committed_rate_accepts_bandwidth_and_the_unmetered_tier() {
        assert_eq!(validate_parse_committed_rate("1Gbps"), Ok(1_000_000_000));
        assert_eq!(validate_parse_committed_rate("unmetered"), Ok(u64::MAX));
        assert_eq!(validate_parse_committed_rate("UNMETERED"), Ok(u64::MAX));
        assert!(validate_parse_committed_rate("1000").is_err());
    }

    #[test]
    fn test_sla_hash_is_a_sha256_digest() {
        assert_eq!(
            validate_parse_sla_hash(&"ab".repeat(32)),
            Ok("ab".repeat(32))
        );
        // The 0x prefix is accepted and dropped, so a digest pasted from a tool that adds one works.
        assert_eq!(
            validate_parse_sla_hash(&format!("0x{}", "ab".repeat(32))),
            Ok("ab".repeat(32))
        );
        assert!(validate_parse_sla_hash("abcd").is_err());
        assert!(validate_parse_sla_hash(&"zz".repeat(32)).is_err());

        assert_eq!(parse_sla_hash(&"09".repeat(32)), [9u8; 32]);
        assert_eq!(parse_sla_hash(&"ff00".repeat(16))[0..2], [255, 0]);
    }
}
