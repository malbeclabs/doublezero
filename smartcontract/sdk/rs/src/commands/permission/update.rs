use crate::DoubleZeroClient;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::permission::update::PermissionUpdateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdatePermissionCommand {
    pub permission_pda: Pubkey,
    pub add: u128,
    pub remove: u128,
}

impl UpdatePermissionCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_authorized_transaction(
            DoubleZeroInstruction::UpdatePermission(PermissionUpdateArgs {
                add: self.add,
                remove: self.remove,
            }),
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
        commands::permission::update::UpdatePermissionCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_permission_pda},
        processors::permission::update::PermissionUpdateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_permission_update_command() {
        let mut client = create_test_client();

        let user_payer = Pubkey::new_unique();
        let add: u128 = 0b110;
        let remove: u128 = 0b001;
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (permission_pda, _) = get_permission_pda(&client.get_program_id(), &user_payer);

        client
            .expect_execute_authorized_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdatePermission(
                    PermissionUpdateArgs { add, remove },
                )),
                predicate::eq(vec![
                    AccountMeta::new(permission_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = UpdatePermissionCommand {
            permission_pda,
            add,
            remove,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
