use clap::{Args, Subcommand};
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::commands::{
    device::list::ListDeviceCommand,
    link::{list::ListLinkCommand, update::UpdateLinkCommand},
    topology::{backfill::BackfillTopologyCommand, list::ListTopologyCommand},
};
use doublezero_serviceability::{pda::get_topology_pda, state::interface::LoopbackType};
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct MigrateCliCommand {
    #[command(subcommand)]
    pub command: MigrateCommands,
}

#[derive(Debug, Subcommand)]
pub enum MigrateCommands {
    /// Backfill link topologies and Vpnv4 loopback FlexAlgoNodeSegments (RFC-18 migration)
    FlexAlgo(FlexAlgoMigrateCliCommand),
}

#[derive(Args, Debug)]
pub struct FlexAlgoMigrateCliCommand {
    /// Print what would be changed without submitting transactions
    #[arg(long, default_value_t = false)]
    pub dry_run: bool,
}

impl FlexAlgoMigrateCliCommand {
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
        let mut links_needing_tag = 0u32;
        let mut links_skipped = 0u32;

        let mut link_entries: Vec<(Pubkey, _)> = links.into_iter().collect();
        link_entries.sort_by_key(|(pk, _)| pk.to_string());

        for (pubkey, link) in &link_entries {
            if link.link_topologies.is_empty() {
                links_needing_tag += 1;
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
                }
            } else {
                links_skipped += 1;
            }
        }

        // ── Part 2: Vpnv4 loopback FlexAlgoNodeSegment backfill ─────────────────

        let topologies = client.list_topology(ListTopologyCommand)?;
        let mut topology_entries: Vec<(Pubkey, _)> = topologies.into_iter().collect();
        topology_entries.sort_by_key(|(pk, _)| pk.to_string());

        let devices = client.list_device(ListDeviceCommand)?;
        let mut device_entries: Vec<(Pubkey, _)> = devices.into_iter().collect();
        device_entries.sort_by_key(|(pk, _)| pk.to_string());

        let mut topologies_backfilled = 0u32;
        let mut topologies_skipped = 0u32;

        for (topology_pubkey, topology) in &topology_entries {
            let mut devices_needing_backfill: Vec<Pubkey> = Vec::new();

            for (device_pubkey, device) in &device_entries {
                let needs_backfill = device.interfaces.iter().any(|iface| {
                    let current = iface.into_current_version();
                    current.loopback_type == LoopbackType::Vpnv4
                        && !current
                            .flex_algo_node_segments
                            .iter()
                            .any(|s| s.topology == *topology_pubkey)
                });
                if needs_backfill {
                    devices_needing_backfill.push(*device_pubkey);
                }
            }

            if devices_needing_backfill.is_empty() {
                topologies_skipped += 1;
                continue;
            }

            topologies_backfilled += 1;
            writeln!(
                out,
                "  [topology] {} ({}) — {} device(s) need backfill",
                topology.name,
                topology_pubkey,
                devices_needing_backfill.len()
            )?;

            if !self.dry_run {
                let result = client.backfill_topology(BackfillTopologyCommand {
                    name: topology.name.clone(),
                    device_pubkeys: devices_needing_backfill,
                });
                match result {
                    Ok(sigs) => {
                        writeln!(out, "    backfilled in {} transaction(s)", sigs.len())?;
                    }
                    Err(e) => {
                        writeln!(
                            out,
                            "    WARNING: failed to backfill topology {}: {e}",
                            topology.name
                        )?;
                    }
                }
            }
        }

        // ── Summary ──────────────────────────────────────────────────────────────

        let dry_run_suffix = if self.dry_run {
            " [DRY RUN — no changes made]"
        } else {
            ""
        };
        let tagged_summary = if self.dry_run {
            format!("{links_needing_tag} link(s) would be tagged")
        } else {
            format!("{links_tagged} link(s) tagged")
        };
        let loopback_summary = if self.dry_run {
            format!("{topologies_backfilled} topology(s) would be backfilled")
        } else {
            format!("{topologies_backfilled} topology(s) backfilled")
        };
        writeln!(
            out,
            "\nMigration complete: {tagged_summary}, {links_skipped} link(s) skipped; {loopback_summary}, {topologies_skipped} topology(s) already complete{dry_run_suffix}"
        )?;

        Ok(())
    }
}
