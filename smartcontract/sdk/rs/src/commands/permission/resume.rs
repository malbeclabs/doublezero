use crate::DoubleZeroClient;
use doublezero_serviceability::processors::permission::resume::PermissionResumeArgs;
use doublezero_serviceability_instruction::permission::resume_permission;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct ResumePermissionCommand {
    pub permission_pda: Pubkey,
}

impl ResumePermissionCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(resume_permission(
            &client.get_program_id(),
            &client.get_payer(),
            &self.permission_pda,
            PermissionResumeArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::permission::resume::ResumePermissionCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_permission_pda, processors::permission::resume::PermissionResumeArgs,
    };
    use doublezero_serviceability_instruction::permission::resume_permission;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_permission_resume_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let user_payer = Pubkey::new_unique();
        let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

        let expected = resume_permission(
            &program_id,
            &payer,
            &permission_pda,
            PermissionResumeArgs {},
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = ResumePermissionCommand { permission_pda }.execute(&client);

        assert!(res.is_ok());
    }
}
