use crate::{
    accesspass::types::CliAccessPassType,
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey,
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::user::delete::DeleteUserCommand;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct DeleteUserCliCommand {
    /// User Pubkey to delete
    #[arg(long, value_parser = validate_pubkey)]
    pub pubkey: String,
    /// The kind of access pass the user holds. Required: the program refuses the call when the
    /// pass is a different kind, so stating it here is what makes the delete targeted.
    #[arg(long = "access-pass-type")]
    pub accesspass_type: CliAccessPassType,
}

impl DeleteUserCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = Pubkey::from_str(&self.pubkey)?;
        let signature = client.delete_user(DeleteUserCommand {
            pubkey,
            kind: self.accesspass_type.into(),
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};

    use crate::{
        accesspass::types::CliAccessPassType,
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
        user::delete::DeleteUserCliCommand,
    };
    use doublezero_sdk::{
        commands::user::{delete::DeleteUserCommand, get::GetUserCommand},
        AccountType, User, UserCYOA, UserStatus, UserType,
    };
    use doublezero_serviceability::{pda::get_user_old_pda, state::accesspass::AccessPassKind};
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_user_delete() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_user_old_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let user = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 255,
            user_type: UserType::IBRL,
            tenant_pk: Pubkey::default(),
            cyoa_type: UserCYOA::GREOverDIA,
            device_pk: Pubkey::default(),
            client_ip: [10, 0, 0, 1].into(),
            dz_ip: [10, 0, 0, 2].into(),
            tunnel_id: 0,
            tunnel_net: "10.2.3.4/24".parse().unwrap(),
            status: UserStatus::Activated,
            owner: pda_pubkey,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            ..Default::default()
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok((pda_pubkey, user.clone())));

        client
            .expect_delete_user()
            .with(predicate::eq(DeleteUserCommand {
                pubkey: pda_pubkey,
                kind: AccessPassKind::Prepaid,
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let res = block_on(
            DeleteUserCliCommand {
                pubkey: pda_pubkey.to_string(),
                accesspass_type: CliAccessPassType::Prepaid,
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
    fn test_cli_user_delete_requires_access_pass_type() {
        use clap::Parser;

        #[derive(Parser, Debug)]
        struct TestCli {
            #[command(subcommand)]
            command: TestCommand,
        }

        #[derive(clap::Subcommand, Debug)]
        enum TestCommand {
            Delete(DeleteUserCliCommand),
        }

        // Omitting --access-pass-type is a parse error: there is no default, so an
        // operator who forgets the flag is stopped here rather than the program
        // guessing a kind.
        let missing_type = TestCli::try_parse_from([
            "test",
            "delete",
            "--pubkey",
            &Pubkey::new_unique().to_string(),
        ]);
        assert!(missing_type.is_err(), "{missing_type:?}");

        let with_type = TestCli::try_parse_from([
            "test",
            "delete",
            "--pubkey",
            &Pubkey::new_unique().to_string(),
            "--access-pass-type",
            "prepaid",
        ]);
        assert!(with_type.is_ok(), "{with_type:?}");
    }
}
