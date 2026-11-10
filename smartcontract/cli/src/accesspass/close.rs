use crate::{
    accesspass::types::CliAccessPassType,
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::accesspass::close::CloseAccessPassCommand;
use doublezero_serviceability::state::accesspass::AccessPassKind;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CloseAccessPassCliCommand {
    /// Access pass public key
    #[arg(long)]
    pub pubkey: Pubkey,
    /// The kind of access pass being closed. Optional: without it the command reads the pass,
    /// prints its kind, and closes it as that kind. With it the program refuses the close when
    /// the pass is a different kind.
    #[arg(long)]
    pub accesspass_type: Option<CliAccessPassType>,
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

        let kind = match self.accesspass_type {
            Some(accesspass_type) => accesspass_type.into(),
            None => {
                let accesspass = client
                    .get_account_data(self.pubkey)?
                    .get_accesspass()
                    .map_err(|e| {
                        eyre::eyre!("Account {} is not an access pass: {e}", self.pubkey)
                    })?;
                let kind = AccessPassKind::from(&accesspass.accesspass_type);
                writeln!(
                    out,
                    "Access pass {} is {}",
                    self.pubkey,
                    CliAccessPassType::from(kind)
                )?;
                kind
            }
        };

        let signature = client.close_accesspass(CloseAccessPassCommand {
            pubkey: self.pubkey,
            kind,
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
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        state::{
            accesspass::{AccessPass, AccessPassKind, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            user::User,
        },
    };
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
                accesspass_type: Some(CliAccessPassType::EdgeSeat),
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

    fn close_test_accesspass(accesspass_type: AccessPassType) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            accesspass_type,
            client_ip: [100, 0, 0, 1].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
            tenant_allowlist: vec![],
            unicast_user_count: 0,
            max_unicast_users: 1,
            public_multicast_user_count: 0,
            max_public_multicast_users: 1,
        }
    }

    #[test]
    fn test_cli_access_pass_close_reads_the_type_when_omitted() {
        // Without the flag the command reads the pass, prints its kind, and closes it as that
        // kind.
        let mut client = create_test_client();
        let accesspass_pubkey = Pubkey::new_unique();
        let signature = Signature::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_account_data()
            .with(predicate::eq(accesspass_pubkey))
            .returning(|_| {
                Ok(AccountData::AccessPass(close_test_accesspass(
                    AccessPassType::SolanaValidator(Pubkey::new_unique()),
                )))
            });
        client
            .expect_close_accesspass()
            .with(predicate::eq(CloseAccessPassCommand {
                pubkey: accesspass_pubkey,
                kind: AccessPassKind::SolanaValidator,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = block_on(
            CloseAccessPassCliCommand {
                pubkey: accesspass_pubkey,
                accesspass_type: None,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        );
        assert!(res.is_ok(), "{res:?}");
        assert_eq!(
            String::from_utf8(output).unwrap(),
            format!(
                "Access pass {accesspass_pubkey} is solana-validator\nSignature: {signature}\n"
            )
        );
    }

    #[test]
    fn test_cli_access_pass_close_refuses_an_account_that_is_not_a_pass() {
        // Without the flag, an address that holds some other account is refused before any
        // transaction is sent.
        let mut client = create_test_client();
        let pubkey = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_account_data()
            .with(predicate::eq(pubkey))
            .returning(|_| Ok(AccountData::User(User::default())));
        client.expect_close_accesspass().times(0);

        let mut output = Vec::new();
        let res = block_on(
            CloseAccessPassCliCommand {
                pubkey,
                accesspass_type: None,
            }
            .execute(&cli_context_default_for_tests(), &client, &mut output),
        );
        let err = res.expect_err("a non-pass account must be refused");
        assert_eq!(
            err.to_string(),
            format!("Account {pubkey} is not an access pass: Invalid Account Type")
        );
    }

    #[test]
    fn test_cli_access_pass_close_type_is_optional() {
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

        let parse = |extra: &[&str]| {
            let pubkey = Pubkey::new_unique().to_string();
            let mut args = vec!["test", "close", "--pubkey", &pubkey];
            args.extend_from_slice(extra);
            TestCli::try_parse_from(args).map(|cli| match cli.command {
                TestCommand::Close(cmd) => cmd.accesspass_type,
            })
        };

        assert_eq!(parse(&[]).unwrap(), None);
        assert_eq!(
            parse(&["--accesspass-type", "edge-seat"]).unwrap(),
            Some(CliAccessPassType::EdgeSeat)
        );
        assert_eq!(
            parse(&["--accesspass-type", "nonsense"])
                .expect_err("an unknown type must fail")
                .kind(),
            clap::error::ErrorKind::InvalidValue,
        );
    }
}
