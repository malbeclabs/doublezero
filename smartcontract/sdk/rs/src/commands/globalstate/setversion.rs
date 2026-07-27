use crate::DoubleZeroClient;
use doublezero_serviceability::{
    processors::globalstate::setversion::SetVersionArgs, programversion::ProgramVersion,
};
use doublezero_serviceability_instruction::globalstate::set_min_version;
use solana_sdk::signature::Signature;

#[derive(Debug, PartialEq, Clone)]
pub struct SetVersionCommand {
    pub min_compatible_version: ProgramVersion,
}

impl SetVersionCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(set_min_version(
            &client.get_program_id(),
            &client.get_payer(),
            SetVersionArgs {
                min_compatible_version: self.min_compatible_version.clone(),
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::globalstate::setversion::SetVersionCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::globalstate::setversion::SetVersionArgs;
    use doublezero_serviceability_instruction::globalstate::set_min_version;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_setauthority_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let expected = set_min_version(
            &program_id,
            &payer,
            SetVersionArgs {
                min_compatible_version: "1.0.0".parse().unwrap(),
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetVersionCommand {
            min_compatible_version: "1.0.0".parse().unwrap(),
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
