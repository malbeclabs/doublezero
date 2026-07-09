use crate::{
    doublezerocommand::CliCommand, helpers::resolve_exchange_arg,
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::commands::feed::{delete::DeleteFeedCommand, get::GetFeedCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteFeedCliCommand {
    /// Feed pubkey or code to delete
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Metro (exchange) pubkey or code to disambiguate a code that exists in multiple metros
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub exchange: Option<String>,
}

impl DeleteFeedCliCommand {
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
            .map(|e| resolve_exchange_arg(client, e))
            .transpose()?;
        let (pubkey, _feed) = client.get_feed(GetFeedCommand {
            pubkey_or_code: self.pubkey,
            exchange,
        })?;

        let signature = client.delete_feed(DeleteFeedCommand { pubkey })?;
        print_signature(out, &signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{feed::delete::DeleteFeedCliCommand, tests::utils::create_test_client};
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::{
            exchange::get::GetExchangeCommand,
            feed::{delete::DeleteFeedCommand, get::GetFeedCommand},
        },
        AccountType, Exchange, ExchangeStatus, Feed,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_feed_delete_with_exchange_code() {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));

        let exchange_pk = Pubkey::new_unique();
        let feed_pk = Pubkey::new_unique();
        let signature = Signature::new_unique();

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "xchi".to_string(),
            name: "Test Exchange".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 12.34,
            lng: 56.78,
            bgp_community: 1,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::new_unique(),
        };
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: "xchi".to_string(),
            }))
            .times(1)
            .returning(move |_| Ok((exchange_pk, exchange.clone())));

        let feed = Feed {
            account_type: AccountType::Feed,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            code: "feed01".to_string(),
            name: "Feed".to_string(),
            exchange: exchange_pk,
            groups: vec![],
        };
        client
            .expect_get_feed()
            .with(predicate::eq(GetFeedCommand {
                pubkey_or_code: "feed01".to_string(),
                exchange: Some(exchange_pk),
            }))
            .times(1)
            .returning(move |_| Ok((feed_pk, feed.clone())));

        client
            .expect_delete_feed()
            .with(predicate::eq(DeleteFeedCommand { pubkey: feed_pk }))
            .times(1)
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            DeleteFeedCliCommand {
                pubkey: "feed01".to_string(),
                exchange: Some("xchi".to_string()),
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
