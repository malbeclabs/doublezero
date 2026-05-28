use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::{render_collection, CliContext, OutputFormat};
use doublezero_program_common::serializer;
use doublezero_sdk::{
    commands::contributor::list::ListContributorCommand, Contributor, ContributorStatus,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct ListContributorCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct ContributorDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub status: ContributorStatus,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListContributorCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let contributors = client.list_contributor(ListContributorCommand {})?;

        let mut contributors: Vec<(Pubkey, Contributor)> = contributors.into_iter().collect();

        contributors.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner));

        let contributor_displays: Vec<ContributorDisplay> = contributors
            .into_iter()
            .map(|(pubkey, tunnel)| ContributorDisplay {
                account: pubkey,
                code: tunnel.code,
                status: tunnel.status,
                owner: tunnel.owner,
            })
            .collect();

        render_collection(
            out,
            contributor_displays,
            OutputFormat::from_flags(self.json, self.json_compact),
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        contributor::list::{ContributorStatus::Activated, ListContributorCliCommand},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{AccountType, Contributor};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_contributor_list() {
        let mut client = create_test_client();

        let contributor1_pubkey =
            Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let contributor1 = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 255,
            code: "some code".to_string(),
            reference_count: 0,
            status: Activated,
            owner: contributor1_pubkey,
            ops_manager_pk: Pubkey::default(),
        };
        client
            .expect_list_contributor()
            .returning(move |_| Ok(HashMap::from([(contributor1_pubkey, contributor1.clone())])));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            ListContributorCliCommand {
                json: false,
                json_compact: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code      | status    | owner                                     \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | some code | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = block_on(
            ListContributorCliCommand {
                json: false,
                json_compact: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"code\":\"some code\",\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }
}
