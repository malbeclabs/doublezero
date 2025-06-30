use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::device::resume::DeviceResumeArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct ResumeDeviceCommand {
    pub pubkey: Pubkey,
}

impl ResumeDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::ResumeDevice(DeviceResumeArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
