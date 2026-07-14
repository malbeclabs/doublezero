use doublezero_serviceability_instruction::globalstate::init_global_state;
use solana_sdk::signature::Signature;

use crate::DoubleZeroClient;

#[derive(Debug, PartialEq, Clone)]
pub struct InitGlobalStateCommand;

impl InitGlobalStateCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(init_global_state(
            &client.get_program_id(),
            &client.get_payer(),
        ))
    }
}
