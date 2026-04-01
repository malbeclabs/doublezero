use clap::Args;
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::commands::{
    device::list::ListDeviceCommand,
    link::{list::ListLinkCommand, update::UpdateLinkCommand},
    topology::list::ListTopologyCommand,
};
use doublezero_serviceability::{pda::get_topology_pda, state::interface::LoopbackType};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashSet, io::Write};

#[derive(Args, Debug)]
pub struct MigrateCliCommand {
    /// Print what would be changed without submitting transactions
    #[arg(long, default_value_t = false)]
    pub dry_run: bool,
}

impl MigrateCliCommand {
    pub fn execute<C: CliCommand, W: Write>(&self, client: &C, out: &mut W) -> eyre::Result<()> {
        let program_id = client.get_program_id();

        // Verify UNICAST-DEFAULT topology PDA exists on chain.
        let (unicast_default_pda, _) = get_topology_pda(&program_id, "UNICAST-DEFAULT");
        client
            .get_account(unicast_default_pda)
            .map_err(|_| eyre::eyre!("UNICAST-DEFAULT topology PDA {unicast_default_pda} not found on chain — cannot proceed"))?;

        // ── Part 1: link topology backfill ───────────────────────────────────────

        let links = client.list_link(ListLinkCommand)?;
        let mut links_tagged = 0u32;
        let mut links_skipped = 0u32;

        let mut link_entries: Vec<(Pubkey, _)> = links.into_iter().collect();
        link_entries.sort_by_key(|(pk, _)| pk.to_string());

        for (pubkey, link) in &link_entries {
            if link.link_topologies.is_empty() {
                writeln!(
                    out,
                    "  [link] {pubkey} ({}) — would tag UNICAST-DEFAULT",
                    link.code
                )?;
                if !self.dry_run {
                    let result = client.update_link(UpdateLinkCommand {
                        pubkey: *pubkey,
                        code: None,
                        contributor_pk: None,
                        tunnel_type: None,
                        bandwidth: None,
                        mtu: None,
                        delay_ns: None,
                        jitter_ns: None,
                        delay_override_ns: None,
                        status: None,
                        desired_status: None,
                        tunnel_id: None,
                        tunnel_net: None,
                        link_topologies: Some(vec![unicast_default_pda]),
                        unicast_drained: None,
                    });
                    match result {
                        Ok(sig) => {
                            links_tagged += 1;
                            writeln!(out, "    tagged: {sig}")?;
                        }
                        Err(e) => {
                            writeln!(out, "    WARNING: failed to tag {pubkey}: {e}")?;
                        }
                    }
                } else {
                    links_tagged += 1;
                }
            } else {
                links_skipped += 1;
            }
        }

        // ── Part 2: Vpnv4 loopback gap reporting ─────────────────────────────────

        let topologies = client.list_topology(ListTopologyCommand)?;
        let topology_pubkeys: HashSet<Pubkey> = topologies.keys().copied().collect();

        let devices = client.list_device(ListDeviceCommand)?;
        let mut loopbacks_with_gaps = 0u32;

        let mut device_entries: Vec<(Pubkey, _)> = devices.into_iter().collect();
        device_entries.sort_by_key(|(pk, _)| pk.to_string());

        for (device_pubkey, device) in &device_entries {
            for iface in &device.interfaces {
                let current = iface.into_current_version();
                if current.loopback_type != LoopbackType::Vpnv4 {
                    continue;
                }

                let present: HashSet<Pubkey> = current
                    .flex_algo_node_segments
                    .iter()
                    .map(|seg| seg.topology)
                    .collect();

                let missing_count = topology_pubkeys.difference(&present).count();
                if missing_count > 0 {
                    loopbacks_with_gaps += 1;
                    writeln!(
                        out,
                        "  [loopback] {device_pubkey} iface={} — missing {missing_count} topology entries; re-create topology with device accounts to backfill",
                        current.name
                    )?;
                }
            }
        }

        // ── Summary ──────────────────────────────────────────────────────────────

        let dry_run_suffix = if self.dry_run {
            " [DRY RUN — no changes made]"
        } else {
            ""
        };
        writeln!(
            out,
            "\nMigration complete: {links_tagged} links tagged, {links_skipped} links skipped, {loopbacks_with_gaps} loopbacks with gaps{dry_run_suffix}"
        )?;

        Ok(())
    }
}
