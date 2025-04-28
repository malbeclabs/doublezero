use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_device_pda,
    processors::device::delete::DeviceDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

pub struct DeleteDeviceCommand {
    pub index: u128,
}

impl DeleteDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) = get_device_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs {
                index: self.index,
                bump_seed,
            }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
