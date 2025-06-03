use crate::doublezerocommand::CliCommand;
use crate::requirements::{CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::bandwidth_parse;
use doublezero_sdk::commands::multicastgroup::create::CreateMulticastGroupCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use std::str::FromStr;

#[derive(Args, Debug)]
pub struct CreateMulticastGroupCliCommand {
    #[arg(long)]
    pub code: String,
    #[arg(long)]
    pub max_bandwidth: String,
    #[arg(long)]
    pub owner: String,
}

impl CreateMulticastGroupCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let owner_pk = {
            if self.owner.eq_ignore_ascii_case("me") {
                client.get_payer()
            } else {
                Pubkey::from_str(&self.owner)?
            }
        };

        let (signature, _pubkey) = client.create_multicastgroup(CreateMulticastGroupCommand {
            code: self.code.clone(),
            max_bandwidth: bandwidth_parse(&self.max_bandwidth),
            owner: owner_pk,
        })?;

        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::doublezerocommand::CliCommand;
    use crate::multicastgroup::create::CreateMulticastGroupCliCommand;
    use crate::requirements::{CHECK_BALANCE, CHECK_ID_JSON};
    use crate::tests::utils::create_test_client;
    use doublezero_sdk::commands::multicastgroup::create::CreateMulticastGroupCommand;
    use doublezero_sdk::get_device_pda;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_multicastgroup_create() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_device_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_create_multicastgroup()
            .with(predicate::eq(CreateMulticastGroupCommand {
                code: "test".to_string(),
                max_bandwidth: 10000000000,
                owner: pda_pubkey,
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = CreateMulticastGroupCliCommand {
            code: "test".to_string(),
            max_bandwidth: "10Gbps".to_string(),
            owner: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
