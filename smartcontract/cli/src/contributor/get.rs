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
            "account: {},\r\ncode: {}\r\nata_owner_pk: {}\r\nstatus: {}\r\nowner: {}",
            pubkey,
            contributor.code,
            contributor.ata_owner_pk,
            contributor.status,
            contributor.owner
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
            ata_owner_pk: Pubkey::default(),
            status: ContributorStatus::Activated,
            owner: contributor1_pubkey,
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

        client.expect_list_contributor().returning(move |_| {
            let mut list = HashMap::new();
            list.insert(contributor1_pubkey, contributor1.clone());
            Ok(list)
        });

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
        assert_eq!(output_str, "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB,\r\ncode: test\r\nata_owner_pk: 11111111111111111111111111111111\r\nstatus: activated\r\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\n");

        // Expected success
        let mut output = Vec::new();
        let res = GetContributorCliCommand {
            code: "test".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB,\r\ncode: test\r\nata_owner_pk: 11111111111111111111111111111111\r\nstatus: activated\r\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\n");
    }
}
