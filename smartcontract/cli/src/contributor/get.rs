use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_cli_core::{render_record, CliContext, OutputFormat};
use doublezero_sdk::commands::contributor::get::GetContributorCommand;
use serde::Serialize;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetContributorCliCommand {
    /// Contributor Pubkey or code to get details for
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct ContributorDisplay {
    pub account: String,
    pub code: String,
    pub reference_count: u32,
    pub status: String,
    pub owner: String,
    pub ops_manager_key: String,
}

impl GetContributorCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let (pubkey, contributor) = client.get_contributor(GetContributorCommand {
            pubkey_or_code: self.code,
        })?;

        let display = ContributorDisplay {
            account: pubkey.to_string(),
            code: contributor.code,
            reference_count: contributor.reference_count,
            status: contributor.status.to_string(),
            owner: contributor.owner.to_string(),
            ops_manager_key: contributor.ops_manager_pk.to_string(),
        };

        render_record(out, &display, OutputFormat::from_flags(self.json, false))
    }
}

#[cfg(test)]
mod tests {
    use crate::{contributor::get::GetContributorCliCommand, tests::utils::create_test_client};
    use doublezero_cli_core::testing::cli_context_default_for_tests;
    use doublezero_sdk::{
        commands::contributor::get::GetContributorCommand, AccountType, Contributor,
        ContributorStatus,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, str::FromStr};
    use tokio::runtime::Builder;

    fn block_on<F: std::future::Future>(f: F) -> F::Output {
        Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(f)
    }

    #[test]
    fn test_cli_contributor_get() {
        let mut client = create_test_client();

        let contributor1_pubkey =
            Pubkey::from_str("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB").unwrap();
        let contributor1 = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            reference_count: 0,
            status: ContributorStatus::Activated,
            owner: contributor1_pubkey,
            ops_manager_pk: Pubkey::default(),
        };

        let contributor2 = contributor1.clone();
        client
            .expect_get_contributor()
            .with(predicate::eq(GetContributorCommand {
                pubkey_or_code: contributor1_pubkey.to_string(),
            }))
            .returning(move |_| Ok((contributor1_pubkey, contributor2.clone())));
        let contributor3 = contributor1.clone();
        client
            .expect_get_contributor()
            .with(predicate::eq(GetContributorCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((contributor1_pubkey, contributor3.clone())));
        client
            .expect_get_contributor()
            .returning(move |_| Err(eyre::eyre!("not found")));

        client
            .expect_list_contributor()
            .returning(move |_| Ok(HashMap::from([(contributor1_pubkey, contributor1.clone())])));

        let ctx = cli_context_default_for_tests();

        // Expected failure
        let mut output = Vec::new();
        let res = block_on(
            GetContributorCliCommand {
                code: Pubkey::new_unique().to_string(),
                json: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success by pubkey (table)
        let mut output = Vec::new();
        let res = block_on(
            GetContributorCliCommand {
                code: contributor1_pubkey.to_string(),
                json: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("account", &contributor1_pubkey.to_string()),
            "account row should contain pubkey"
        );
        assert!(has_row("code", "test"), "code row should contain value");
        assert!(
            has_row("status", "activated"),
            "status row should contain value"
        );

        // Expected success by code (JSON)
        let mut output = Vec::new();
        let res = block_on(
            GetContributorCliCommand {
                code: "test".to_string(),
                json: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "I should find a item by code");
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(
            json["account"].as_str().unwrap(),
            contributor1_pubkey.to_string()
        );
        assert_eq!(json["code"].as_str().unwrap(), "test");
        assert_eq!(json["status"].as_str().unwrap(), "activated");
        assert_eq!(json["reference_count"].as_u64().unwrap(), 0);
    }
}
