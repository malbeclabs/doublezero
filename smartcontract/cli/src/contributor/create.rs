use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_code, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_sdk::commands::contributor::{
    create::CreateContributorCommand, list::ListContributorCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct CreateContributorCliCommand {
    /// Unique contributor code
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Owner of the contributor
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub owner: String,
}

impl CreateContributorCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let contributors = client.list_contributor(ListContributorCommand {})?;
        if contributors.iter().any(|(_, d)| d.code == self.code) {
            return Err(eyre::eyre!(
                "Contributor with code '{}' already exists",
                self.code
            ));
        }
        // Create contributor
        let owner = {
            if self.owner.eq_ignore_ascii_case("me") {
                client.get_payer()
            } else {
                Pubkey::from_str(&self.owner)?
            }
        };

        let (signature, _pubkey) = client.create_contributor(CreateContributorCommand {
            code: self.code.clone(),
            owner,
        })?;

        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        contributor::create::CreateContributorCliCommand,
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::contributor::{create::CreateContributorCommand, list::ListContributorCommand},
        get_contributor_pda, AccountType, Contributor, ContributorStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_contributor_create() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_contributor_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_list_contributor()
            .with(predicate::eq(ListContributorCommand {}))
            .returning(move |_| {
                Ok(vec![(
                    pda_pubkey,
                    Contributor {
                        account_type: AccountType::Contributor,
                        owner: Pubkey::default(),
                        index: 1,
                        reference_count: 0,
                        code: "test2".to_string(),
                        status: ContributorStatus::Activated,
                        bump_seed: 0,
                        ops_manager_pk: Pubkey::default(),
                    },
                )]
                .into_iter()
                .collect())
            });
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_create_contributor()
            .with(predicate::eq(CreateContributorCommand {
                code: "test".to_string(),
                owner: Pubkey::default(),
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = CreateContributorCliCommand {
            code: "test2".to_string(),
            owner: Pubkey::default().to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_err());

        let mut output = Vec::new();
        let res = CreateContributorCliCommand {
            code: "test".to_string(),
            owner: Pubkey::default().to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
