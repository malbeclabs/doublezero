use crate::{DoubleZeroClient, GetGlobalStateCommand};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_resource_extension_pda,
    processors::resource::allocate::ResourceAllocateArgs, resource::IpBlockType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct AllocateResourceCommand {
    pub ip_block_type: IpBlockType,
    pub requested_network: Option<NetworkV4>,
}

impl AllocateResourceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (resource_pubkey, _, _) =
            get_resource_extension_pda(&client.get_program_id(), self.ip_block_type);

        let resource_allocate_args = ResourceAllocateArgs {
            ip_block_type: self.ip_block_type,
            requested_network: self.requested_network,
        };

        let associated_account_pk = match self.ip_block_type {
            IpBlockType::DzPrefixBlock(pk, _) => pk,
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
