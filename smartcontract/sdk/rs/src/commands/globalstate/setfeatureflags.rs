use crate::DoubleZeroClient;
use doublezero_serviceability::processors::globalstate::setfeatureflags::SetFeatureFlagsArgs;
use doublezero_serviceability_instruction::globalstate::set_feature_flags;
use solana_sdk::signature::Signature;

#[derive(Clone, Debug, PartialEq)]
pub struct SetFeatureFlagsCommand {
    pub feature_flags: u128,
}

impl SetFeatureFlagsCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(set_feature_flags(
            &client.get_program_id(),
            &client.get_payer(),
            SetFeatureFlagsArgs {
                feature_flags: self.feature_flags,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::globalstate::setfeatureflags::SetFeatureFlagsCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::processors::globalstate::setfeatureflags::SetFeatureFlagsArgs;
    use doublezero_serviceability_instruction::globalstate::set_feature_flags;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_setfeatureflags_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let feature_flags = 1u128;

        let expected =
            set_feature_flags(&program_id, &payer, SetFeatureFlagsArgs { feature_flags });
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetFeatureFlagsCommand { feature_flags }.execute(&client);
        assert!(res.is_ok());
    }
}
