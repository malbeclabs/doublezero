use clap::Args;
use doublezero_sdk::commands::user::get::GetUserCommand;
use doublezero_sdk::*;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use std::str::FromStr;
use crate::doublezerocommand::CliCommand;

#[derive(Args, Debug)]
pub struct GetUserCliCommand {
    #[arg(long)]
    pub pubkey: String,
}

impl GetUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let pubkey = Pubkey::from_str(&self.pubkey)?;
        let (pubkey, user) =client.get_user(GetUserCommand { pubkey })?;

        writeln!(out, 
                "account: {} user_type: {} device: {} cyoa_type: {} client_ip: {} tunnel_net: {} dz_ip: {} status: {} owner: {}",
                pubkey,
                user.user_type,
                user.device_pk,
                user.cyoa_type,
                ipv4_to_string(&user.client_ip),
                networkv4_to_string(&user.tunnel_net),
                ipv4_to_string(&user.dz_ip),
                user.status,
                user.owner
            )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::doublezerocommand::CliCommand;
    use crate::tests::tests::create_test_client;
    use crate::user::get::GetUserCliCommand;
    use doublezero_sdk::commands::user::delete::DeleteUserCommand;
    use doublezero_sdk::commands::user::get::GetUserCommand;
    use doublezero_sdk::AccountType;
    use doublezero_sdk::User;
    use doublezero_sdk::UserCYOA;
    use doublezero_sdk::UserStatus;
    use doublezero_sdk::UserType;
    use doublezero_sla_program::pda::get_user_pda;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_user_get() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_user_pda(&client.get_program_id(), 1);
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
            client_ip: [10, 0, 0, 1],
            dz_ip: [10, 0, 0, 2],
            tunnel_id: 0,
            tunnel_net: ([10, 2, 3, 4], 24),
            status: UserStatus::Activated,
            owner: pda_pubkey,
            publishers: vec![],
            subscribers: vec![],
        };

        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok((pda_pubkey, user.clone())));

        client
            .expect_delete_user()
            .with(predicate::eq(DeleteUserCommand { index: 1 }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        // Expected success 
        let mut output = Vec::new();
        let res = GetUserCliCommand {
            pubkey: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: CJTXjCEbDDgQoccJgEbNGc63QwWzJtdAoSio36zVXHQw user_type: IBRL device: 11111111111111111111111111111111 cyoa_type: GREOverDIA client_ip: 10.0.0.1 tunnel_net: 10.2.3.4/24 dz_ip: 10.0.0.2 status: activated owner: CJTXjCEbDDgQoccJgEbNGc63QwWzJtdAoSio36zVXHQw\n");

    }
}
