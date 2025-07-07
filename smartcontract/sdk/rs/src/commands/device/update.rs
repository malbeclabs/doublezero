use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::device::update::DeviceUpdateArgs,
    state::device::{DeviceType, Interface},
    types::NetworkV4List,
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
    pub bgp_asn: Option<u32>,
    pub dia_bgp_asn: Option<u32>,
    pub mgmt_vrf: Option<String>,
    pub dns_servers: Option<Vec<std::net::Ipv4Addr>>,
    pub ntp_servers: Option<Vec<std::net::Ipv4Addr>>,
    pub interfaces: Option<Vec<Interface>>,
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
                bgp_asn: self.bgp_asn,
                dia_bgp_asn: self.dia_bgp_asn,
                mgmt_vrf: self.mgmt_vrf.clone(),
                dns_servers: self.dns_servers.clone(),
                ntp_servers: self.ntp_servers.clone(),
                interfaces: self.interfaces.clone(),
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
