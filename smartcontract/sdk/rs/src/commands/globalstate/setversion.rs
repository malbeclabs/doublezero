use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::globalstate::setversion::SetVersionArgs,
    programversion::ProgramVersion,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetVersionCommand {
    pub min_compatible_version: ProgramVersion,
}

impl SetVersionCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::SetMinVersion(SetVersionArgs {
                min_compatible_version: self.min_compatible_version.clone(),
            }),
            vec![AccountMeta::new(globalstate_pubkey, false)],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::globalstate::setversion::SetVersionCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
        processors::globalstate::setversion::SetVersionArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_setauthority_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetMinVersion(SetVersionArgs {
                    min_compatible_version: "1.0.0".parse().unwrap(),
                })),
                predicate::eq(vec![AccountMeta::new(globalstate_pubkey, false)]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetVersionCommand {
            min_compatible_version: "1.0.0".parse().unwrap(),
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
