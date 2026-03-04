use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
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
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
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

        if self.json {
            let json = serde_json::to_string_pretty(&display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = ContributorDisplay::headers();
            let fields = display.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{contributor::get::GetContributorCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{
        commands::contributor::get::GetContributorCommand, AccountType, Contributor,
        ContributorStatus,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, str::FromStr};

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

        // Expected failure
        let mut output = Vec::new();
        let res = GetContributorCliCommand {
            code: Pubkey::new_unique().to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success by pubkey (table)
        let mut output = Vec::new();
        let res = GetContributorCliCommand {
            code: contributor1_pubkey.to_string(),
            json: false,
        }
        .execute(&client, &mut output);
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
        let res = GetContributorCliCommand {
            code: "test".to_string(),
            json: true,
        }
        .execute(&client, &mut output);
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
