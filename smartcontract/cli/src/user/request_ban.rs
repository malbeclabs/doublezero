use std::str::FromStr;

use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_FOUNDATION_ALLOWLIST, CHECK_ID_JSON},
    validators::validate_pubkey,
};
use clap::Args;
use doublezero_sdk::commands::user::requestban::RequestBanUserCommand;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct RequestBanUserCliCommand {
    /// User Pubkey to ban
    #[arg(long, value_parser = validate_pubkey)]
    pub pubkey: String,
}

impl RequestBanUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE | CHECK_FOUNDATION_ALLOWLIST)?;

        let pubkey = Pubkey::from_str(&self.pubkey)?;
        let signature = client.request_ban_user(RequestBanUserCommand { pubkey })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_FOUNDATION_ALLOWLIST, CHECK_ID_JSON},
        tests::utils::create_test_client,
        user::request_ban::RequestBanUserCliCommand,
    };
    use doublezero_sdk::{
        commands::user::{
            delete::DeleteUserCommand, get::GetUserCommand, requestban::RequestBanUserCommand,
        },
        AccountType, User, UserCYOA, UserStatus, UserType,
    };
    use doublezero_serviceability::pda::get_user_old_pda;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_user_request_ban() {
        let mut client = create_test_client();

        let payer = client.get_payer();
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
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(
                CHECK_ID_JSON | CHECK_BALANCE | CHECK_FOUNDATION_ALLOWLIST,
            ))
            .returning(|_| Ok(()));
        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok((pda_pubkey, user.clone())));

        client
            .expect_delete_user()
            .with(predicate::eq(DeleteUserCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok(signature));
        client
            .expect_list_foundation_allowlist()
            .returning(move |_| Ok(vec![payer]));

        client
            .expect_request_ban_user()
            .with(predicate::eq(RequestBanUserCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = RequestBanUserCliCommand {
            pubkey: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
