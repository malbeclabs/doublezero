use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::allowlist::qa::remove::RemoveQaAllowlistCommand;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct RemoveQaCliCommand {
    /// QA Pubkey to remove from the allowlist
    #[arg(long)]
    pub pubkey: String,
}

impl RemoveQaCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = {
            if self.pubkey.eq_ignore_ascii_case("me") {
                client.get_payer()
            } else {
                Pubkey::from_str(&self.pubkey)?
            }
        };

        let signature = client.remove_qa_allowlist(RemoveQaAllowlistCommand { pubkey })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        allowlist::qa::remove::RemoveQaCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::commands::allowlist::qa::remove::RemoveQaAllowlistCommand;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_qa_allowlist_remove() {
        let mut client = create_test_client();

        let pubkey = Pubkey::new_unique();
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
            .expect_remove_qa_allowlist()
            .with(predicate::eq(RemoveQaAllowlistCommand { pubkey }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = RemoveQaCliCommand {
            pubkey: pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
