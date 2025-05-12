use doublezero_sla_program::{
    instructions::DoubleZeroInstruction,
    pda::get_device_pda,
    processors::device::create::DeviceCreateArgs,
    state::device::DeviceType,
    types::{IpV4, NetworkV4List},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateDeviceCommand {
    pub code: String,
    pub location_pk: Pubkey,
    pub exchange_pk: Pubkey,
    pub device_type: DeviceType,
    pub public_ip: IpV4,
    pub dz_prefixes: NetworkV4List,
}

impl CreateDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) =
            get_device_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                    index: globalstate.account_index + 1,
                    bump_seed,
                    code: self.code.clone(),
                    location_pk: self.location_pk,
                    exchange_pk: self.exchange_pk,
                    device_type: self.device_type,
                    public_ip: self.public_ip,
                    dz_prefixes: self.dz_prefixes.clone(),
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.location_pk, false),
                    AccountMeta::new(self.exchange_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
