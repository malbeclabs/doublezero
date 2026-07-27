use crate::DoubleZeroClient;
use doublezero_serviceability::{
    pda::get_permission_pda, processors::permission::create::PermissionCreateArgs,
};
use doublezero_serviceability_instruction::permission::create_permission;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreatePermissionCommand {
    pub user_payer: Pubkey,
    pub permissions: u128,
}

impl CreatePermissionCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let program_id = client.get_program_id();
        // The builder derives the target permission PDA from args.user_payer; we
        // derive it here too for the returned pubkey.
        let (permission_pda, _) = get_permission_pda(&program_id, &self.user_payer);

        let ix = create_permission(
            &program_id,
            &client.get_payer(),
            PermissionCreateArgs {
                user_payer: self.user_payer,
                permissions: self.permissions,
            },
        );

        client.send_transaction(ix).map(|sig| (sig, permission_pda))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::permission::create::CreatePermissionCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_permission_pda, processors::permission::create::PermissionCreateArgs,
    };
    use doublezero_serviceability_instruction::permission::create_permission;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_permission_create_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let user_payer = Pubkey::new_unique();
        let permissions: u128 = 0b11;
        let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

        let expected = create_permission(
            &program_id,
            &payer,
            PermissionCreateArgs {
                user_payer,
                permissions,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CreatePermissionCommand {
            user_payer,
            permissions,
        }
        .execute(&client);

        assert!(res.is_ok());
        let (_, returned_pda) = res.unwrap();
        assert_eq!(returned_pda, permission_pda);
    }
}
