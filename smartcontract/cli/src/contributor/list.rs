use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::{commands::contributor::list::ListContributorCommand, *};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

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
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub ata_owner: String,
    pub status: ContributorStatus,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListContributorCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let contributors = client.list_contributor(ListContributorCommand {})?;

        let mut contributors: Vec<(Pubkey, Contributor)> = contributors.into_iter().collect();

        contributors.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner));

        let contributor_displays: Vec<ContributorDisplay> = contributors
            .into_iter()
            .map(|(pubkey, tunnel)| ContributorDisplay {
                account: pubkey,
                code: tunnel.code,
                ata_owner: tunnel.ata_owner_pk.to_string(),
                status: tunnel.status,
                owner: tunnel.owner,
            })
            .collect();

        let res = if self.json {
            serde_json::to_string_pretty(&contributor_displays)?
        } else if self.json_compact {
            serde_json::to_string(&contributor_displays)?
        } else {
            Table::new(contributor_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        contributor::list::{ContributorStatus::Activated, ListContributorCliCommand},
        tests::utils::create_test_client,
    };
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
            ata_owner_pk: Pubkey::default(),
            status: Activated,
            owner: contributor1_pubkey,
        };
        client.expect_list_contributor().returning(move |_| {
            let mut contributors = HashMap::new();
            contributors.insert(contributor1_pubkey, contributor1.clone());
            Ok(contributors)
        });

        let mut output = Vec::new();
        let res = ListContributorCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code      | ata_owner                        | status    | owner                                     \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | some code | 11111111111111111111111111111111 | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = ListContributorCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"code\":\"some code\",\"ata_owner\":\"11111111111111111111111111111111\",\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }
}
