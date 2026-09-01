use crate::{
    accesspass::types::CliAccessPassType,
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::accesspass::close::CloseAccessPassCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CloseAccessPassCliCommand {
    /// Access pass public key
    #[arg(long)]
    pub pubkey: Pubkey,
    /// The kind of access pass being closed. Required: the program refuses the call when the
    /// pass is a different kind, so stating it here is what makes the close targeted.
    #[arg(long = "type")]
    pub accesspass_type: CliAccessPassType,
}

impl CloseAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = client.close_accesspass(CloseAccessPassCommand {
            pubkey: self.pubkey,
            kind: self.accesspass_type.into(),
        })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        accesspass::{close::CloseAccessPassCliCommand, types::CliAccessPassType},
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::commands::accesspass::close::CloseAccessPassCommand;
    use doublezero_serviceability::{pda::get_accesspass_pda, state::accesspass::AccessPassKind};
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_device_create() {
        let mut client = create_test_client();

        let client_ip = [100, 0, 0, 1].into();
        let payer = Pubkey::new_unique();

        let (_pda_pubkey, _bump_seed) =
            get_accesspass_pda(&client.get_program_id(), &client_ip, &payer);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let accesspass_pubkey = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_close_accesspass()
            .with(predicate::eq(CloseAccessPassCommand {
                pubkey: accesspass_pubkey,
                kind: AccessPassKind::EdgeSeat,
            }))
            .returning(move |_| Ok(signature));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            CloseAccessPassCliCommand {
                pubkey: accesspass_pubkey,
                accesspass_type: CliAccessPassType::EdgeSeat,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_access_pass_type_maps_to_kind() {
        let cases = [
            (CliAccessPassType::Prepaid, AccessPassKind::Prepaid),
            (
                CliAccessPassType::SolanaValidator,
                AccessPassKind::SolanaValidator,
            ),
            (CliAccessPassType::SolanaRPC, AccessPassKind::SolanaRPC),
            (CliAccessPassType::Others, AccessPassKind::Others),
            (CliAccessPassType::EdgeSeat, AccessPassKind::EdgeSeat),
        ];
        for (cli, want) in cases {
            assert_eq!(AccessPassKind::from(cli), want, "{cli:?}");
        }
    }

    #[test]
    fn test_cli_access_pass_close_requires_type() {
        use clap::Parser;

        #[derive(Parser, Debug)]
        struct TestCli {
            #[command(subcommand)]
            command: TestCommand,
        }

        #[derive(clap::Subcommand, Debug)]
        enum TestCommand {
            Close(CloseAccessPassCliCommand),
        }

        // Omitting --type is a parse error: there is no default, so an operator who
        // forgets the flag is stopped here rather than the program guessing a kind.
        let missing_type = TestCli::try_parse_from([
            "test",
            "close",
            "--pubkey",
            &Pubkey::new_unique().to_string(),
        ]);
        assert!(missing_type.is_err(), "{missing_type:?}");

        let with_type = TestCli::try_parse_from([
            "test",
            "close",
            "--pubkey",
            &Pubkey::new_unique().to_string(),
            "--type",
            "prepaid",
        ]);
        assert!(with_type.is_ok(), "{with_type:?}");
    }
}
