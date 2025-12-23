use crate::{DoubleZeroClient, GetGlobalStateCommand};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalconfig_pda, get_resource_extension_pda},
    processors::resource::create::ResourceCreateArgs,
    resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateResourceCommand {
    pub resource_type: ResourceType,
}

impl CreateResourceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (globalconfig_pubkey, _) = get_globalconfig_pda(&client.get_program_id());

        let (resource_pubkey, _, _) =
            get_resource_extension_pda(&client.get_program_id(), self.resource_type);

        let resource_create_args = ResourceCreateArgs {
            resource_type: self.resource_type,
        };

        let associated_account_pk = match self.resource_type {
            ResourceType::DzPrefixBlock(pk, _) | ResourceType::TunnelIds(pk, _) => pk,
            _ => Pubkey::default(),
        };

        client.execute_transaction(
            DoubleZeroInstruction::CreateResource(resource_create_args),
            vec![
                AccountMeta::new(resource_pubkey, false),
                AccountMeta::new(associated_account_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(globalconfig_pubkey, false),
            ],
        )
    }
}
