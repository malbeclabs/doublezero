use crate::{DoubleZeroClient, GetGlobalStateCommand};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_resource_extension_pda,
    processors::resource::allocate::ResourceAllocateArgs,
    resource::{IdOrIp, ResourceType},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct AllocateResourceCommand {
    pub resource_type: ResourceType,
    pub requested: Option<IdOrIp>,
}

impl AllocateResourceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (resource_pubkey, _, _) =
            get_resource_extension_pda(&client.get_program_id(), self.resource_type);

        let resource_allocate_args = ResourceAllocateArgs {
            resource_type: self.resource_type,
            requested: self.requested.clone(),
        };

        let associated_account_pk = match self.resource_type {
            ResourceType::DzPrefixBlock(pk, _) | ResourceType::TunnelIds(pk, _) => pk,
            _ => Pubkey::default(),
        };

        client.execute_transaction(
            DoubleZeroInstruction::AllocateResource(resource_allocate_args),
            vec![
                AccountMeta::new(resource_pubkey, false),
                AccountMeta::new(associated_account_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
