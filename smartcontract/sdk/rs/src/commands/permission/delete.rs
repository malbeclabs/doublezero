use crate::DoubleZeroClient;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::permission::delete::PermissionDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeletePermissionCommand {
    pub permission_pda: Pubkey,
}

impl DeletePermissionCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::DeletePermission(PermissionDeleteArgs {}),
            vec![
                AccountMeta::new(self.permission_pda, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::permission::delete::DeletePermissionCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_permission_pda},
        processors::permission::delete::PermissionDeleteArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_permission_delete_command() {
        let mut client = create_test_client();

        let user_payer = Pubkey::new_unique();
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (permission_pda, _) = get_permission_pda(&client.get_program_id(), &user_payer);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeletePermission(
                    PermissionDeleteArgs {},
                )),
                predicate::eq(vec![
                    AccountMeta::new(permission_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeletePermissionCommand { permission_pda }.execute(&client);

        assert!(res.is_ok());
    }
}
