use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::globalstate::setfeatureflags::SetFeatureFlagsArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

#[derive(Clone, Debug, PartialEq)]
pub struct SetFeatureFlagsCommand {
    pub feature_flags: u128,
}

impl SetFeatureFlagsCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("GlobalState not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
                feature_flags: self.feature_flags,
            }),
            vec![AccountMeta::new(globalstate_pubkey, false)],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::globalstate::setfeatureflags::SetFeatureFlagsCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
        processors::globalstate::setfeatureflags::SetFeatureFlagsArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_setfeatureflags_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());

        let feature_flags = 1u128;

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetFeatureFlags(
                    SetFeatureFlagsArgs { feature_flags },
                )),
                predicate::eq(vec![AccountMeta::new(globalstate_pubkey, false)]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetFeatureFlagsCommand { feature_flags }.execute(&client);
        assert!(res.is_ok());
    }
}
