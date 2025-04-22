use double_zero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_tunnel_pda,
    processors::tunnel::update::TunnelUpdateArgs, state::tunnel::TunnelTunnelType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::DoubleZeroClient;

pub struct UpdateTunnelCommand {
    pub index: u128,
    pub code: Option<String>,
    pub tunnel_type: Option<TunnelTunnelType>,
    pub bandwidth: Option<u64>,
    pub mtu: Option<u32>,
    pub delay_ns: Option<u64>,
    pub jitter_ns: Option<u64>,
}

impl UpdateTunnelCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (pda_pubkey, _) = get_tunnel_pda(&client.get_program_id(), self.index);
        client
            .execute_transaction(
                DoubleZeroInstruction::UpdateTunnel(TunnelUpdateArgs {
                    index: self.index,
                    code: self.code.clone(),
                    tunnel_type: self.tunnel_type,
                    bandwidth: self.bandwidth,
                    mtu: self.mtu,
                    delay_ns: self.delay_ns,
                    jitter_ns: self.jitter_ns,
                }),
                vec![AccountMeta::new(pda_pubkey, false)],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
