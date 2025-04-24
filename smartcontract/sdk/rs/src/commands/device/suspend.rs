use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_device_pda,
    processors::device::suspend::DeviceSuspendArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

pub struct SuspendDeviceCommand {
    pub index: u128,
}

impl SuspendDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_device_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::SuspendDevice(DeviceSuspendArgs { index: self.index }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
