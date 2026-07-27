use crate::DoubleZeroClient;
use doublezero_serviceability::processors::permission::suspend::PermissionSuspendArgs;
use doublezero_serviceability_instruction::permission::suspend_permission;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SuspendPermissionCommand {
    pub permission_pda: Pubkey,
}

impl SuspendPermissionCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(suspend_permission(
            &client.get_program_id(),
            &client.get_payer(),
            &self.permission_pda,
            PermissionSuspendArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::permission::suspend::SuspendPermissionCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_permission_pda, processors::permission::suspend::PermissionSuspendArgs,
    };
    use doublezero_serviceability_instruction::permission::suspend_permission;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_permission_suspend_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let user_payer = Pubkey::new_unique();
        let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

        let expected = suspend_permission(
            &program_id,
            &payer,
            &permission_pda,
            PermissionSuspendArgs {},
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SuspendPermissionCommand { permission_pda }.execute(&client);

        assert!(res.is_ok());
    }
}
