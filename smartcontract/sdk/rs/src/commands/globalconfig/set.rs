use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_globalconfig_pda,
    processors::globalconfig::set::SetGlobalConfigArgs, types::NetworkV4,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{DoubleZeroClient, GetGlobalStateCommand};

#[derive(Debug, PartialEq, Clone)]
pub struct SetGlobalConfigCommand {
    pub local_asn: u32,
    pub remote_asn: u32,
    pub tunnel_tunnel_block: NetworkV4,
    pub user_tunnel_block: NetworkV4,
}

impl SetGlobalConfigCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
        client.execute_transaction(
            DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
                local_asn: self.local_asn,
                remote_asn: self.remote_asn,
                tunnel_tunnel_block: self.tunnel_tunnel_block,
                user_tunnel_block: self.user_tunnel_block,
            }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
