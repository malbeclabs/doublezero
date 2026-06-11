use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, resource::get::GetResourceCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::resource::closeaccount::ResourceExtensionCloseAccountArgs, resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

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
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::CloseResource(ResourceExtensionCloseAccountArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(self.owner, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
