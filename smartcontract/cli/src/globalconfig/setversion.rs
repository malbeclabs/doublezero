use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::{commands::globalstate::setversion::SetVersionCommand, ProgramVersion};
use std::io::Write;

#[derive(Args, Debug)]
pub struct SetVersionCliCommand {
    /// Minimum compatible client version (e.g., 1.0.0)
    #[clap(long)]
    pub min_compatible_version: ProgramVersion,
}

impl SetVersionCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = client.set_minversion(SetVersionCommand {
            min_compatible_version: self.min_compatible_version,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        globalconfig::setversion::SetVersionCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::commands::globalstate::setversion::SetVersionCommand;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_set_min_version() {
        let mut client = create_test_client();

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
            .expect_set_minversion()
            .with(predicate::eq(SetVersionCommand {
                min_compatible_version: "1.0.0".parse().unwrap(),
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        // Set all global config; reflects initializing global config or updating all config values
        let mut output1 = Vec::new();
        let res = SetVersionCliCommand {
            min_compatible_version: "1.0.0".parse().unwrap(),
        }
        .execute(&client, &mut output1);
        assert!(res.is_ok());
        let output_str1 = String::from_utf8(output1).unwrap();
        assert_eq!(
            output_str1,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
