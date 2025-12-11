use crate::{DoubleZeroClient, GetGlobalStateCommand};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_device_tunnel_block_pda, get_globalconfig_pda},
    processors::resource::allocate::{IpBlockType, ResourceAllocateArgs},
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct AllocateResourceCommand {}

impl AllocateResourceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (globalconfig_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
        let (resource_pubkey, _) = get_device_tunnel_block_pda(&client.get_program_id());

        let resource_allocate_args = ResourceAllocateArgs {
            ip_block_type: IpBlockType::DeviceTunnelBlock,
        };

        client.execute_transaction(
            DoubleZeroInstruction::AllocateResource(resource_allocate_args),
            vec![
                AccountMeta::new(resource_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(globalconfig_pubkey, false),
            ],
        )
    }
}
