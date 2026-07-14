use crate::DoubleZeroClient;
use doublezero_serviceability::{
    processors::resource::create::ResourceCreateArgs, resource::ResourceType,
};
use doublezero_serviceability_instruction::resource::create_resource;
use solana_sdk::signature::Signature;

#[derive(Debug, PartialEq, Clone)]
pub struct CreateResourceCommand {
    pub resource_type: ResourceType,
}

impl CreateResourceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(create_resource(
            &client.get_program_id(),
            &client.get_payer(),
            ResourceCreateArgs {
                resource_type: self.resource_type,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use mockall::predicate;

    #[test]
    fn test_commands_resource_create() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let cmd = CreateResourceCommand {
            resource_type: ResourceType::DeviceTunnelBlock,
        };
        let expected = create_resource(
            &program_id,
            &payer,
            ResourceCreateArgs {
                resource_type: cmd.resource_type,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        assert!(cmd.execute(&client).is_ok());
    }
}
