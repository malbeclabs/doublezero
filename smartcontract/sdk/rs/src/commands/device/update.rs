use double_zero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_device_pda,
    processors::device::update::DeviceUpdateArgs, state::device::DeviceType, types::{IpV4, NetworkV4List},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

pub struct UpdateDeviceCommand {
    pub index: u128,
    pub code: Option<String>,
    pub device_type: Option<DeviceType>,
    pub public_ip: Option<IpV4>,
    pub dz_prefixes: Option<NetworkV4List>
}

impl UpdateDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_device_pda(&client.get_program_id(), self.index);
        client
            .execute_transaction(
                DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                    index: self.index,
                    code: self.code.clone(),
                    device_type: self.device_type,
                    public_ip: self.public_ip,
                    dz_prefixes: self.dz_prefixes.clone(),
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
