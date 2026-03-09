use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_permission_pda,
    processors::permission::create::PermissionCreateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreatePermissionCommand {
    pub user_payer: Pubkey,
    pub permissions: u128,
}

impl CreatePermissionCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (permission_pda, _) = get_permission_pda(&client.get_program_id(), &self.user_payer);

        client
            .execute_authorized_transaction(
                DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
                    user_payer: self.user_payer,
                    permissions: self.permissions,
                }),
                vec![
                    AccountMeta::new(permission_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, permission_pda))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::permission::create::CreatePermissionCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_permission_pda},
        processors::permission::create::PermissionCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_permission_create_command() {
        let mut client = create_test_client();

        let user_payer = Pubkey::new_unique();
        let permissions: u128 = 0b11;
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (permission_pda, _) = get_permission_pda(&client.get_program_id(), &user_payer);

        client
            .expect_execute_authorized_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreatePermission(
                    PermissionCreateArgs {
                        user_payer,
                        permissions,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(permission_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

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
