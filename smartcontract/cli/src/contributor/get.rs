use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_sdk::commands::contributor::get::GetContributorCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetContributorCliCommand {
    /// Contributor Pubkey or code to get details for
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
}

impl GetContributorCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, contributor) = client.get_contributor(GetContributorCommand {
            pubkey_or_code: self.code,
        })?;

        writeln!(
            out,
            "account: {},\r\ncode: {}\r\nreference_count: {}\r\nstatus: {}\r\nowner: {}\r\nops_manager_key: {}",
            pubkey,
            contributor.code,
            contributor.reference_count,
            contributor.status,
            contributor.owner,
            contributor.ops_manager_pk
        )?;

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

        /*****************************************************************************************************/
        // Expected failure
        let mut output = Vec::new();
        let res = GetContributorCliCommand {
            code: Pubkey::new_unique().to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success
        let mut output = Vec::new();
        let res = GetContributorCliCommand {
            code: contributor1_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB,\r\ncode: test\r\nreference_count: 0\r\nstatus: activated\r\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\r\nops_manager_key: 11111111111111111111111111111111\n");

        // Expected success
        let mut output = Vec::new();
        let res = GetContributorCliCommand {
            code: "test".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB,\r\ncode: test\r\nreference_count: 0\r\nstatus: activated\r\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\r\nops_manager_key: 11111111111111111111111111111111\n");
    }
}
