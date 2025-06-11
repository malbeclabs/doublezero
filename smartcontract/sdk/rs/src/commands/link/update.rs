use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_link_pda,
    processors::link::update::LinkUpdateArgs, state::link::LinkLinkType,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateLinkCommand {
    pub index: u128,
    pub code: Option<String>,
    pub tunnel_type: Option<LinkLinkType>,
    pub bandwidth: Option<u64>,
    pub mtu: Option<u32>,
    pub delay_ns: Option<u64>,
    pub jitter_ns: Option<u64>,
}

impl UpdateLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, bump_seed) = get_link_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
                index: self.index,
                bump_seed,
                code: self.code.clone(),
                tunnel_type: self.tunnel_type,
                bandwidth: self.bandwidth,
                mtu: self.mtu,
                delay_ns: self.delay_ns,
                jitter_ns: self.jitter_ns,
            }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
