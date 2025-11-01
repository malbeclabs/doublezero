use crate::{
    commands::link::{get::GetLinkCommand, list::ListLinkCommand},
    telemetry::{calculate_stats, get_all_device_latency_samples, LinkLatencyStats},
    DoubleZeroClient,
};
use doublezero_telemetry::{
    pda::derive_device_latency_samples_pda, state::device_latency_samples::DeviceLatencySamples,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct LatencyLinkCommand {
    pub pubkey_or_code: Option<String>,
    pub epoch: Option<u64>,
    pub telemetry_program_id: Pubkey,
}

impl LatencyLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<LinkLatencyStats>> {
        // Get current or specified epoch
        let epoch = match self.epoch {
            Some(e) => e,
            None => client.get_epoch()?,
        };

        match &self.pubkey_or_code {
            Some(code) => {
                let (link_pk, link) = GetLinkCommand {
                    pubkey_or_code: code.clone(),
                }
                .execute(client)?;

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
                    Some(link.code.clone()),
                    link.side_a_pk,
                    link.side_z_pk,
                    &latency_data.samples,
                )?;

                Ok(vec![stats])
            }
            None => {
                // All links case
                let all_links = ListLinkCommand.execute(client)?;
                let all_telemetry =
                    get_all_device_latency_samples(client, &self.telemetry_program_id, epoch)?;

                let mut results = Vec::new();
                for (link_pk, link) in all_links {
                    // Find telemetry data for this link
                    if let Some(telemetry_data) =
                        all_telemetry.values().find(|t| t.header.link_pk == link_pk)
                    {
                        let stats = calculate_stats(
                            epoch,
                            link_pk,
                            Some(link.code.clone()),
                            link.side_a_pk,
                            link.side_z_pk,
                            &telemetry_data.samples,
                        )?;

                        results.push(stats);
                    }
                }

                Ok(results)
            }
        }
    }
}
