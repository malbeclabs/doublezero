use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::permission::resume::ResumePermissionCommand;
use doublezero_serviceability::pda::get_permission_pda;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct ResumePermissionCliCommand {
    /// Pubkey to resume permissions for
    #[arg(long)]
    pub user_payer: String,
}

impl ResumePermissionCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let user_payer = Pubkey::from_str(&self.user_payer)
            .map_err(|e| eyre::eyre!("invalid user_payer pubkey: {e}"))?;

        let program_id = client.get_program_id();
        let (permission_pda, _) = get_permission_pda(&program_id, &user_payer);

        let signature = client.resume_permission(ResumePermissionCommand { permission_pda })?;

        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        permission::resume::ResumePermissionCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::commands::permission::resume::ResumePermissionCommand;
    use doublezero_serviceability::pda::get_permission_pda;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    const TEST_PROGRAM_ID: Pubkey =
        Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");

    #[test]
    fn test_cli_permission_resume() {
        let mut client = create_test_client();
        let user_payer = Pubkey::new_unique();
        let (permission_pda, _) = get_permission_pda(&TEST_PROGRAM_ID, &user_payer);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_resume_permission()
            .with(predicate::eq(ResumePermissionCommand { permission_pda }))
            .returning(|_| Ok(Signature::new_unique()));

        let mut output = Vec::new();
        let res = ResumePermissionCliCommand {
            user_payer: user_payer.to_string(),
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        assert!(String::from_utf8(output).unwrap().contains("Signature:"));
    }
}
