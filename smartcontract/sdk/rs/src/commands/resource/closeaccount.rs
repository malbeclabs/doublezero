use crate::{commands::resource::get::GetResourceCommand, DoubleZeroClient};
use doublezero_serviceability::{
    processors::resource::closeaccount::ResourceExtensionCloseAccountArgs, resource::ResourceType,
};
use doublezero_serviceability_instruction::resource::close_resource;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CloseResourceCommand {
    pub resource_type: ResourceType,
}

impl CloseResourceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pubkey, resource) = GetResourceCommand {
            resource_type: self.resource_type,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device not found"))?;

        CloseResourceByPubkeyCommand {
            pubkey,
            owner: resource.owner,
        }
        .execute(client)
    }
}

/// Close a resource extension identified directly by its PDA. Used to clean up
/// orphaned extensions whose `ResourceType` is no longer derivable from current
/// onchain state (e.g., extensions belonging to deleted devices).
#[derive(Debug, PartialEq, Clone)]
pub struct CloseResourceByPubkeyCommand {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
}

impl CloseResourceByPubkeyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(close_resource(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            &self.owner,
            ResourceExtensionCloseAccountArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_resource_close_by_pubkey() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let resource = Pubkey::new_unique();
        let owner = Pubkey::new_unique();

        let expected = close_resource(
            &program_id,
            &payer,
            &resource,
            &owner,
            ResourceExtensionCloseAccountArgs {},
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        assert!(CloseResourceByPubkeyCommand {
            pubkey: resource,
            owner,
        }
        .execute(&client)
        .is_ok());
    }
}
