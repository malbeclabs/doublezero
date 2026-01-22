use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, resource::get::GetResourceCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::resource::closeaccount::ResourceExtensionCloseAccountArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CloseResourceCommand {
    pub resource_type: ResourceType,
}

impl CloseResourceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pubkey, resource) = GetResourceCommand {
            resource_type: self.resource_type,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device not found"))?;

        client.execute_transaction(
            DoubleZeroInstruction::CloseResource(ResourceExtensionCloseAccountArgs {}),
            vec![
                AccountMeta::new(pubkey, false),
                AccountMeta::new(resource.owner, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
