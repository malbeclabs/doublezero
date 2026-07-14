use crate::DoubleZeroClient;
use doublezero_serviceability::processors::permission::update::PermissionUpdateArgs;
use doublezero_serviceability_instruction::permission::update_permission;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdatePermissionCommand {
    pub permission_pda: Pubkey,
    pub add: u128,
    pub remove: u128,
}

impl UpdatePermissionCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(update_permission(
            &client.get_program_id(),
            &client.get_payer(),
            &self.permission_pda,
            PermissionUpdateArgs {
                add: self.add,
                remove: self.remove,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::permission::update::UpdatePermissionCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_permission_pda, processors::permission::update::PermissionUpdateArgs,
    };
    use doublezero_serviceability_instruction::permission::update_permission;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_permission_update_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let user_payer = Pubkey::new_unique();
        let add: u128 = 0b110;
        let remove: u128 = 0b001;
        let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

        let expected = update_permission(
            &program_id,
            &payer,
            &permission_pda,
            PermissionUpdateArgs { add, remove },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdatePermissionCommand {
            permission_pda,
            add,
            remove,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
