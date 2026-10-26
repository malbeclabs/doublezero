use crate::{
    doublezerocommand::CliCommand,
    helpers::{resolve_exchange_pk, resolve_multicastgroup_pk},
    validators::{validate_code, validate_parse_bandwidth, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::{
    commands::feed::create::{CreateFeedCommand, FeedBondTerms},
    FeedChain,
};
use eyre::WrapErr;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
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
    /// Chain this feed publishes for
    #[arg(long, default_value_t = FeedChain::Unspecified)]
    pub chain: FeedChain,
    /// The BuilderBond on Solana behind this feed. Makes it a bonded feed, which also needs
    /// --spec-id and --committed-rate, and takes no --chain
    #[arg(long, requires_all = ["spec_id", "committed_rate"], conflicts_with = "chain")]
    pub bond_ref: Option<Pubkey>,
    /// The edge-feed-spec wire format this feed conforms to, as <spec>@<version>
    #[arg(long, requires = "bond_ref")]
    pub spec_id: Option<String>,
    /// SHA-256 of the declared service level, as 64 hex characters. Left out, the feed declares
    /// none
    #[arg(long, requires = "bond_ref", value_parser = parse_sla_hash)]
    pub sla_hash: Option<[u8; 32]>,
    /// The rate the feed commits to (e.g. 1Gbps). The bond's tier must cover it
    #[arg(long, requires = "bond_ref", value_parser = validate_parse_bandwidth)]
    pub committed_rate: Option<u64>,
    /// The builder the feed belongs to. Defaults to you. Name another key only when creating a
    /// bonded feed on a builder's behalf, which needs feed-authority or foundation
    #[arg(long, requires = "bond_ref")]
    pub builder: Option<Pubkey>,
}

/// Parse a SHA-256 written as 64 hex characters.
fn parse_sla_hash(val: &str) -> Result<[u8; 32], String> {
    let bytes = hex::decode(val).map_err(|err| format!("not hex: {err}"))?;
    bytes
        .try_into()
        .map_err(|bytes: Vec<u8>| format!("a SHA-256 is 32 bytes, this is {}", bytes.len()))
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

        // clap requires --spec-id and --committed-rate with --bond-ref; this only turns that into
        // the command's shape, where the terms travel together. --sla-hash may be left out.
        let bond = match self.bond_ref {
            None => None,
            Some(bond_ref) => Some(FeedBondTerms {
                builder: self.builder.unwrap_or_else(|| client.get_payer()),
                bond_ref,
                spec_id: self
                    .spec_id
                    .ok_or_else(|| eyre::eyre!("--spec-id is required"))?,
                sla_hash: self.sla_hash.unwrap_or_default(),
                committed_rate_bits_per_sec: self
                    .committed_rate
                    .ok_or_else(|| eyre::eyre!("--committed-rate is required"))?,
            }),
        };

        let (signature, _pubkey) = client.create_feed(CreateFeedCommand {
            code: self.code,
            name: self.name,
            exchange,
            groups,
            feed_chain: self.chain,
            bond,
        })?;

        print_signature(out, &signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        feed::create::{parse_sla_hash, CreateFeedCliCommand},
        tests::utils::create_test_client,
    };
    use clap::Parser;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::{
            exchange::get::GetExchangeCommand,
            feed::create::{CreateFeedCommand, FeedBondTerms},
            multicastgroup::get::GetMulticastGroupCommand,
        },
        AccountType, Exchange, ExchangeStatus, FeedChain, MulticastGroup, MulticastGroupStatus,
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
            bond_ref: Pubkey::default(),
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
                feed_chain: FeedChain::Solana,
                bond: None,
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
                chain: FeedChain::Solana,
                bond_ref: None,
                spec_id: None,
                sla_hash: None,
                committed_rate: None,
                builder: None,
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
                feed_chain: FeedChain::Unspecified,
                bond: None,
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
                chain: FeedChain::Unspecified,
                bond_ref: None,
                spec_id: None,
                sla_hash: None,
                committed_rate: None,
                builder: None,
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
                chain: FeedChain::Unspecified,
                bond_ref: None,
                spec_id: None,
                sla_hash: None,
                committed_rate: None,
                builder: None,
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
                chain: FeedChain::Unspecified,
                bond_ref: None,
                spec_id: None,
                sla_hash: None,
                committed_rate: None,
                builder: None,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("Exchange not found: nope"),
            "unexpected error: {err}"
        );
    }

    /// The bond terms reach the command, and the builder defaults to the payer.
    #[test]
    fn test_cli_feed_create_bonded() {
        create_bonded(Some([0xab; 32]), [0xab; 32]);
    }

    /// With no --sla-hash, the command carries all zeros: the feed declares no service level.
    #[test]
    fn test_cli_feed_create_bonded_without_a_service_level() {
        create_bonded(None, [0u8; 32]);
    }

    fn create_bonded(sla_hash: Option<[u8; 32]>, expected: [u8; 32]) {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));
        let payer = client.get_payer();

        let exchange_pk = Pubkey::new_unique();
        let group_pk = Pubkey::new_unique();
        let bond_ref = Pubkey::new_unique();
        let exchange = test_exchange("xchi");
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
                feed_chain: FeedChain::Unspecified,
                bond: Some(FeedBondTerms {
                    builder: payer,
                    bond_ref,
                    spec_id: "top-of-book@v1.0.0".to_string(),
                    sla_hash: expected,
                    committed_rate_bits_per_sec: 1_000_000_000,
                }),
            }))
            .times(1)
            .returning(|_| Ok((Signature::new_unique(), Pubkey::new_unique())));

        let mut out = Vec::new();
        let res = block_on(
            CreateFeedCliCommand {
                code: "tokyo1".to_string(),
                name: "Tokyo".to_string(),
                exchange: exchange_pk.to_string(),
                groups: vec![group_pk.to_string()],
                chain: FeedChain::Unspecified,
                bond_ref: Some(bond_ref),
                spec_id: Some("top-of-book@v1.0.0".to_string()),
                sla_hash,
                committed_rate: Some(1_000_000_000),
                builder: None,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut out),
        );
        assert!(res.is_ok(), "{res:?}");
    }

    #[test]
    fn test_parse_sla_hash() {
        assert_eq!(parse_sla_hash(&"ab".repeat(32)), Ok([0xab; 32]));
        assert!(parse_sla_hash(&"ab".repeat(31)).is_err(), "31 bytes");
        assert!(parse_sla_hash(&"zz".repeat(32)).is_err(), "not hex");
    }

    /// clap holds the terms together: the spec and rate with --bond-ref, none without, and no
    /// --chain. The hash alone may be left out.
    #[test]
    fn test_cli_feed_create_bond_flags_travel_together() {
        #[derive(clap::Parser)]
        struct Cli {
            #[command(flatten)]
            create: CreateFeedCliCommand,
        }
        let base = [
            "feed",
            "--code",
            "tokyo1",
            "--name",
            "Tokyo",
            "--exchange",
            "xchi",
            "--group",
            "mg01",
        ];
        let bond_ref = Pubkey::new_unique().to_string();
        let hash = "ab".repeat(32);
        let parse = |extra: &[&str]| {
            Cli::try_parse_from(base.iter().copied().chain(extra.iter().copied()))
                .map(|cli| cli.create)
        };

        let all = [
            "--bond-ref",
            bond_ref.as_str(),
            "--spec-id",
            "top-of-book@v1.0.0",
            "--sla-hash",
            hash.as_str(),
            "--committed-rate",
            "1Gbps",
        ];
        let create = parse(&all).expect("every term given");
        assert_eq!(create.committed_rate, Some(1_000_000_000));
        assert_eq!(create.sla_hash, Some([0xab; 32]));

        assert_eq!(
            parse(&all[..4]).err().map(|err| err.kind()),
            Some(clap::error::ErrorKind::MissingRequiredArgument),
            "a bond without its rate"
        );
        let no_sla = [&all[..4], &all[6..]].concat();
        assert_eq!(
            parse(&no_sla)
                .expect("a bond with no service level")
                .sla_hash,
            None
        );
        assert!(
            parse(&["--spec-id", "top-of-book@v1.0.0"]).is_err(),
            "a term without a bond"
        );
        let mut with_chain = all.to_vec();
        with_chain.extend(["--chain", "solana"]);
        assert!(parse(&with_chain).is_err(), "a bonded feed names no chain");
        assert!(parse(&[]).is_ok(), "a catalog feed takes none of them");
    }
}
