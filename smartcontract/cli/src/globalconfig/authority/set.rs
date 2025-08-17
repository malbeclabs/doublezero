use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::globalstate::setauthority::SetAuthorityCommand;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct SetAuthorityCliCommand {
    /// New activator authority public key
    #[arg(long)]
    pub activator_authority: Option<String>,

    /// New sentinel authority public key
    #[arg(long)]
    pub sentinel_authority: Option<String>,
}

impl SetAuthorityCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let activator_authority_pk = {
            if let Some(activator_authority) = &self.activator_authority {
                if activator_authority.eq_ignore_ascii_case("me") {
                    Some(client.get_payer())
                } else {
                    Some(Pubkey::from_str(activator_authority)?)
                }
            } else {
                None
            }
        };
        let sentinel_authority_pk = {
            if let Some(sentinel_authority) = &self.sentinel_authority {
                if sentinel_authority.eq_ignore_ascii_case("me") {
                    Some(client.get_payer())
                } else {
                    Some(Pubkey::from_str(sentinel_authority)?)
                }
            } else {
                None
            }
        };

        let signature = client.set_authority(SetAuthorityCommand {
            activator_authority_pk,
            sentinel_authority_pk,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        globalconfig::authority::set::SetAuthorityCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::commands::globalstate::setauthority::SetAuthorityCommand;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_globalconfig_set() {
        let mut client = create_test_client();

        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let activator_authority_pk = Pubkey::new_unique();
        let sentinel_authority_pk = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_authority()
            .with(predicate::eq(SetAuthorityCommand {
                activator_authority_pk: Some(activator_authority_pk),
                sentinel_authority_pk: Some(sentinel_authority_pk),
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        // Set all global config; reflects initializing global config or updating all config values
        let mut output1 = Vec::new();
        let res = SetAuthorityCliCommand {
            activator_authority: Some(activator_authority_pk.to_string()),
            sentinel_authority: Some(sentinel_authority_pk.to_string()),
        }
        .execute(&client, &mut output1);
        assert!(res.is_ok());
        let output_str1 = String::from_utf8(output1).unwrap();
        assert_eq!(
            output_str1,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
