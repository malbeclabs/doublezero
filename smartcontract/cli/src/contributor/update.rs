use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_code, validate_pubkey, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_sdk::commands::contributor::{
    get::GetContributorCommand, list::ListContributorCommand, update::UpdateContributorCommand,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct UpdateContributorCliCommand {
    /// Contributor Pubkey to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Updated code for the contributor
    #[arg(long, value_parser = validate_code)]
    pub code: Option<String>,
    /// Updated owner for the contributor
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub owner: Option<String>,
    /// Updated ops manager pubkey for the contributor
    #[arg(long, value_parser = validate_pubkey)]
    pub ops_manager: Option<String>,
}

impl UpdateContributorCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = Pubkey::from_str(&self.pubkey)
            .map_err(|_| eyre::eyre!("Invalid contributor pubkey"))?;

        if let Some(code) = &self.code {
            let contributors = client.list_contributor(ListContributorCommand {})?;
            if contributors
                .iter()
                .any(|(pk, d)| *pk != pubkey && d.code == *code)
            {
                return Err(eyre::eyre!(
                    "Contributor with code '{}' already exists",
                    code
                ));
            }
        }

        let owner = if let Some(owner_str) = &self.owner {
            Some(Pubkey::from_str(owner_str).map_err(|_| eyre::eyre!("Invalid owner public key"))?)
        } else {
            None
        };

        let ops_manager_pk = if let Some(ops_manager_str) = &self.ops_manager {
            Some(
                Pubkey::from_str(ops_manager_str)
                    .map_err(|_| eyre::eyre!("Invalid ops manager public key"))?,
            )
        } else {
            None
        };

        let (pubkey, _) = client.get_contributor(GetContributorCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let signature = client.update_contributor(UpdateContributorCommand {
            pubkey,
            code: self.code,
            owner,
            ops_manager_pk,
        })?;

        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        contributor::update::UpdateContributorCliCommand,
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::contributor::{
            get::GetContributorCommand, list::ListContributorCommand,
            update::UpdateContributorCommand,
        },
        get_contributor_pda, AccountType, Contributor, ContributorStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_contributor_update() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_contributor_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            reference_count: 0,
            status: ContributorStatus::Activated,
            owner: Pubkey::new_unique(),
            ops_manager_pk: Pubkey::default(),
        };

        client
            .expect_list_contributor()
            .with(predicate::eq(ListContributorCommand {}))
            .returning(move |_| {
                Ok(vec![
                    (
                        pda_pubkey,
                        Contributor {
                            account_type: AccountType::Contributor,
                            owner: Pubkey::default(),
                            index: 1,
                            reference_count: 0,
                            code: "test".to_string(),
                            status: ContributorStatus::Activated,
                            bump_seed: 0,
                            ops_manager_pk: Pubkey::default(),
                        },
                    ),
                    (
                        Pubkey::new_unique(),
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
                    ),
                ]
                .into_iter()
                .collect())
            });
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_contributor()
            .with(predicate::eq(GetContributorCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, contributor.clone())));

        let ops_manager_pk = Pubkey::new_unique();
        client
            .expect_update_contributor()
            .with(predicate::eq(UpdateContributorCommand {
                pubkey: pda_pubkey,
                code: Some("test_new".to_string()),
                owner: Some(Pubkey::default()),
                ops_manager_pk: Some(ops_manager_pk),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        // Expected error
        let mut output = Vec::new();
        let res = UpdateContributorCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("test2".to_string()),
            owner: Some(Pubkey::default().to_string()),
            ops_manager: Some(ops_manager_pk.to_string()),
        }
        .execute(&client, &mut output);
        assert!(res.is_err());

        // Expected success
        let mut output = Vec::new();
        let res = UpdateContributorCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("test_new".to_string()),
            owner: Some(Pubkey::default().to_string()),
            ops_manager: Some(ops_manager_pk.to_string()),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
