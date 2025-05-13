use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_multicastgroup_pda,
    processors::multicastgroup::update::MulticastGroupUpdateArgs, types::IpV4,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateMulticastGroupCommand {
    pub index: u128,
    pub code: Option<String>,
    pub multicast_ip: Option<IpV4>,
    pub max_bandwidth: Option<u64>,
}

impl UpdateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, bump_seed) = get_multicastgroup_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
                index: self.index,
                bump_seed,
                code: self.code.clone(),
                multicast_ip: self.multicast_ip,
                max_bandwidth: self.max_bandwidth,
            }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
