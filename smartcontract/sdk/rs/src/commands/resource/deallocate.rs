use crate::{DoubleZeroClient, GetGlobalStateCommand};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::resource::deallocate::ResourceDeallocateArgs, resource::IpBlockType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeallocateResourceCommand {
    pub ip_block_type: IpBlockType,
    pub network: NetworkV4,
}

impl DeallocateResourceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (resource_pubkey, _, _) =
            get_resource_extension_pda(&client.get_program_id(), self.ip_block_type);

        let resource_deallocate_args = ResourceDeallocateArgs {
            ip_block_type: self.ip_block_type,
            network: self.network,
        };

        let associated_account_pk = match self.ip_block_type {
            IpBlockType::DzPrefixBlock(pk, _) => pk,
            _ => Pubkey::default(),
        };

        client.execute_transaction(
            DoubleZeroInstruction::DeallocateResource(resource_deallocate_args),
            vec![
                AccountMeta::new(resource_pubkey, false),
                AccountMeta::new(associated_account_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
