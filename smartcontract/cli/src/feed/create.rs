use crate::{
    doublezerocommand::CliCommand,
    helpers::{resolve_exchange_arg, resolve_multicastgroup_arg},
    validators::{validate_code, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::create::CreateFeedCommand;
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

        let exchange = resolve_exchange_arg(client, &self.exchange)?;
        let groups = self
            .groups
            .iter()
            .map(|g| resolve_multicastgroup_arg(client, g))
            .collect::<eyre::Result<Vec<_>>>()?;

        let (signature, _pubkey) = client.create_feed(CreateFeedCommand {
            code: self.code,
            name: self.name,
            exchange,
            groups,
        })?;

        print_signature(out, &signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{feed::create::CreateFeedCliCommand, tests::utils::create_test_client};
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::{
            exchange::get::GetExchangeCommand, feed::create::CreateFeedCommand,
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

        // Fixed full-length pubkeys: `Pubkey::new_unique()` can render to fewer than 43 base58
        // chars, which `parse_pubkey` rejects and would wrongly route down the code path.
        let exchange_pk = Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
        let group_pk = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        // Pubkey inputs are used as-is: no get_exchange/get_multicastgroup lookups.
        client
            .expect_create_feed()
            .with(predicate::eq(CreateFeedCommand {
                code: "feed01".to_string(),
                name: "Feed".to_string(),
                exchange: exchange_pk,
                groups: vec![group_pk],
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
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
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
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("Exchange not found: nope"),
            "unexpected error: {err}"
        );
    }
}
