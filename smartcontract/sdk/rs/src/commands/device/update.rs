use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::device::update::DeviceUpdateArgs,
    state::device::DeviceType, types::NetworkV4List,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateDeviceCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub device_type: Option<DeviceType>,
    pub public_ip: Option<Ipv4Addr>,
    pub dz_prefixes: Option<NetworkV4List>,
    pub metrics_publisher: Option<Pubkey>,
    pub contributor_pk: Option<Pubkey>,
}

impl UpdateDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                code: self.code.clone(),
                contributor_pk: self.contributor_pk,
                device_type: self.device_type,
                public_ip: self.public_ip,
                dz_prefixes: self.dz_prefixes.clone(),
                metrics_publisher_pk: self.metrics_publisher,
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
