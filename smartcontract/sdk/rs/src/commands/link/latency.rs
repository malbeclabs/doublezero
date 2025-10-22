use crate::{
    commands::link::get::GetLinkCommand,
    telemetry::{calculate_stats, LinkLatencyStats},
    DoubleZeroClient,
};
use doublezero_telemetry::{
    pda::derive_device_latency_samples_pda, state::device_latency_samples::DeviceLatencySamples,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct LatencyLinkCommand {
    pub pubkey_or_code: String,
    pub epoch: Option<u64>,
    pub telemetry_program_id: Pubkey,
}

impl LatencyLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<LinkLatencyStats> {
        // Get link
        let (link_pk, link) = GetLinkCommand {
            pubkey_or_code: self.pubkey_or_code.clone(),
        }
        .execute(client)?;

        // Get current or specified epoch
        let epoch = match self.epoch {
            Some(e) => e,
            None => client.get_epoch()?,
        };

        let (pda, _bump) = derive_device_latency_samples_pda(
            &self.telemetry_program_id,
            &link.side_a_pk,
            &link.side_z_pk,
            &link_pk,
            epoch,
        );

        let account = client.get_account(pda)?;

        // Get latency data
        let latency_data = DeviceLatencySamples::try_from(&account.data[..])?;

        let stats = calculate_stats(
            epoch,
            link_pk,
            link.side_a_pk,
            link.side_z_pk,
            &latency_data.samples,
        )?;

        Ok(stats)
    }
}
