use crate::DoubleZeroClient;

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::globalstate::setinternetlatencycollector::SetInternetLatencyCollectorArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Clone, Debug, PartialEq)]
pub struct SetInternetLatencyCollectorCommand {
    pub pubkey: Pubkey,
}

impl SetInternetLatencyCollectorCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::SetInternetLatencyCollector(SetInternetLatencyCollectorArgs {
                pubkey: self.pubkey,
            }),
            vec![AccountMeta::new(globalstate_pubkey, false)],
        )
    }
}
