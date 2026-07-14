use crate::DoubleZeroClient;
use doublezero_serviceability::{
    processors::resource::allocate::ResourceAllocateArgs,
    resource::{IdOrIp, ResourceType},
};
use doublezero_serviceability_instruction::resource::allocate_resource;
use solana_sdk::signature::Signature;

#[derive(Debug, PartialEq, Clone)]
pub struct AllocateResourceCommand {
    pub resource_type: ResourceType,
    pub requested: Option<IdOrIp>,
}

impl AllocateResourceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(allocate_resource(
            &client.get_program_id(),
            &client.get_payer(),
            ResourceAllocateArgs {
                resource_type: self.resource_type,
                requested: self.requested.clone(),
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
    fn test_commands_resource_allocate() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let cmd = AllocateResourceCommand {
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: None,
        };
        let expected = allocate_resource(
            &program_id,
            &payer,
            ResourceAllocateArgs {
                resource_type: cmd.resource_type,
                requested: cmd.requested.clone(),
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        assert!(cmd.execute(&client).is_ok());
    }
}
