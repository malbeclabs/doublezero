use double_zero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_device_pda,
    processors::device::deactivate::DeviceDeactivateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::accountdata::getglobalstate::GetGlobalStateCommand, DoubleZeroClient};

pub struct DeactivateDeviceCommand {
    pub index: u128,
    pub owner: Pubkey,
}

impl DeactivateDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_device_pda(&client.get_program_id(), self.index);
        client
            .execute_transaction(
                DoubleZeroInstruction::DeactivateDevice(DeviceDeactivateArgs { index: self.index }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.owner, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            
    }
}
