use crate::{
    commands::{device::get::GetDeviceCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::device::closeaccount::DeviceCloseAccountArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CloseAccountDeviceCommand {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
}

impl CloseAccountDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device not found"))?;

        if device.reference_count > 0 {
            return Err(eyre::eyre!(
                "Device cannot be closed, it has {} references",
                device.reference_count
            ));
        }

        client.execute_transaction(
            DoubleZeroInstruction::CloseAccountDevice(DeviceCloseAccountArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(self.owner, false),
                AccountMeta::new(device.contributor_pk, false),
                AccountMeta::new(device.location_pk, false),
                AccountMeta::new(device.exchange_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
