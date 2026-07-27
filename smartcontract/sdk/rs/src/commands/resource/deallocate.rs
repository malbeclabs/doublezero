use crate::DoubleZeroClient;
use doublezero_serviceability::{
    processors::resource::deallocate::ResourceDeallocateArgs,
    resource::{IdOrIp, ResourceType},
};
use doublezero_serviceability_instruction::resource::deallocate_resource;
use solana_sdk::signature::Signature;

#[derive(Debug, PartialEq, Clone)]
pub struct DeallocateResourceCommand {
    pub resource_type: ResourceType,
    pub value: IdOrIp,
}

impl DeallocateResourceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(deallocate_resource(
            &client.get_program_id(),
            &client.get_payer(),
            ResourceDeallocateArgs {
                resource_type: self.resource_type,
                value: self.value.clone(),
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
    fn test_commands_resource_deallocate() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let cmd = DeallocateResourceCommand {
            resource_type: ResourceType::LinkIds,
            value: IdOrIp::Id(1),
        };
        let expected = deallocate_resource(
            &program_id,
            &payer,
            ResourceDeallocateArgs {
                resource_type: cmd.resource_type,
                value: cmd.value.clone(),
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        assert!(cmd.execute(&client).is_ok());
    }
}
