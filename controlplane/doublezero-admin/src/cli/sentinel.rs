use std::{collections::HashMap, io::Write, net::Ipv4Addr};

use clap::{Args, Subcommand};
use doublezero_sdk::{DZClient, UserType};
use doublezero_sentinel::{
    dz_ledger_reader::{self, DzDeviceInfo, DzLedgerReader, DzUser, RpcDzLedgerReader},
    dz_ledger_writer::build_create_multicast_publisher_instructions,
    multicast_create::{find_candidates, CandidateFilters},
    multicast_find::{apply_filters, FindFilters},
    nearest_device::find_nearest_device_for_multicast,
    output::{print_table, OutputOptions},
    validator_metadata_reader::{
        HttpValidatorMetadataReader, ValidatorMetadataReader, DEFAULT_VALIDATOR_METADATA_URL,
    },
};
use doublezero_serviceability::pda::get_tenant_pda;
use serde::Serialize;
use solana_client::{
    nonblocking::rpc_client::RpcClient as NonblockingRpcClient, rpc_client::RpcClient,
};
use solana_sdk::{
    commitment_config::CommitmentConfig,
    instruction::Instruction,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::Transaction,
};
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct SentinelCliCommand {
    #[command(subcommand)]
    pub command: SentinelCommands,
}

#[derive(Debug, Subcommand)]
pub enum SentinelCommands {
    /// Find IBRL validators and their multicast publisher status.
    FindValidatorMulticastPublishers(FindValidatorMulticastPublishersCommand),
    /// Create multicast publisher users for IBRL validators that don't have one yet.
    CreateValidatorMulticastPublishers(CreateValidatorMulticastPublishersCommand),
}

// ---------------------------------------------------------------------------
// Find command
// ---------------------------------------------------------------------------

#[derive(Serialize, Tabled)]
struct ValidatorPublisherRow {
    #[tabled(rename = "OWNER")]
    owner: String,
    #[tabled(rename = "CLIENT IP")]
    client_ip: String,
    #[tabled(rename = "DEVICE")]
    device: String,
    #[tabled(rename = "NEAREST DEVICE")]
    nearest_device: String,
    #[tabled(rename = "VOTE ACCOUNT")]
    vote_account: String,
    #[tabled(rename = "STAKE (SOL)")]
    stake_sol: String,
    #[tabled(rename = "CLIENT")]
    client: String,
    #[tabled(rename = "VERSION")]
    version: String,
    #[tabled(rename = "PUB")]
    is_publisher: String,
}

#[derive(Serialize, Tabled)]
struct SummaryRow {
    #[tabled(rename = "CLIENT")]
    client: String,
    #[tabled(rename = "VALIDATORS")]
    validators: usize,
    #[tabled(rename = "ON DZ")]
    on_dz: usize,
    #[tabled(rename = "NOT ON DZ")]
    not_on_dz: usize,
    #[tabled(rename = "PUB")]
    publishers: usize,
    #[tabled(rename = "NOT PUB")]
    not_publishers: usize,
}

/// Find IBRL validators and their multicast publisher status.
#[derive(Debug, Args)]
pub struct FindValidatorMulticastPublishersCommand {
    /// Filter by multicast group (pubkey or code, e.g. "edge-solana-shreds").
    #[arg(long, value_name = "KEY_OR_CODE")]
    multicast_group: Option<String>,

    /// Only show validators that are already a publisher.
    #[arg(long, conflicts_with = "not_publisher")]
    is_publisher: bool,

    /// Only show validators that are NOT a publisher.
    #[arg(long, conflicts_with = "is_publisher")]
    not_publisher: bool,

    /// Minimum activated stake in SOL to include.
    #[arg(long, value_name = "SOL")]
    min_stake: Option<f64>,

    /// Maximum activated stake in SOL to include.
    #[arg(long, value_name = "SOL")]
    max_stake: Option<f64>,

    /// Filter by validator client name (e.g. "JitoLabs", "AgaveBam", "Frankendancer"). Repeatable.
    #[arg(long, value_name = "NAME")]
    client: Vec<String>,

    /// Include validators not yet connected to DZ.
    #[arg(long)]
    include_not_on_dz: bool,

    /// Show a summary breakdown by client type instead of per-validator rows.
    #[arg(long)]
    summary: bool,

    /// Use geographic distance (Haversine) instead of onchain link latency for nearest-device.
    #[arg(long)]
    nearest_via_geo: bool,

    /// Validator metadata service URL.
    #[arg(long, value_name = "URL", default_value = DEFAULT_VALIDATOR_METADATA_URL)]
    validator_metadata_url: String,

    #[command(flatten)]
    output: OutputOptions,
}

impl FindValidatorMulticastPublishersCommand {
    pub async fn execute(self, dzclient: &DZClient) -> eyre::Result<()> {
        let program_id = *dzclient.get_program_id();
        let rpc_client = dzclient.rpc_client();

        let device_infos: HashMap<Pubkey, DzDeviceInfo> =
            dz_ledger_reader::fetch_device_infos(rpc_client, &program_id).unwrap_or_default();

        let latency_map = if self.nearest_via_geo {
            None
        } else {
            let telemetry_id = dzclient
                .get_environment()
                .config()
                .ok()
                .map(|c| c.telemetry_program_id);
            telemetry_id.and_then(|tid| {
                dz_ledger_reader::fetch_device_latency_map(rpc_client, &tid)
                    .map_err(|e| eprintln!("Warning: could not fetch latency data: {e}"))
                    .ok()
            })
        };

        let validator_metadata = HttpValidatorMetadataReader {
            api_url: self.validator_metadata_url.clone(),
        };
        let dz_ledger = RpcDzLedgerReader::new(
            NonblockingRpcClient::new_with_commitment(
                dzclient.get_rpc().clone(),
                CommitmentConfig::confirmed(),
            ),
            program_id,
        );

        // Resolve multicast group filter (pubkey or code).
        let multicast_group_pk = match &self.multicast_group {
            Some(key_or_code) => Some(
                dz_ledger_reader::resolve_multicast_group(key_or_code, &dz_ledger)
                    .await
                    .map_err(|e| eyre::eyre!(e))?,
            ),
            None => None,
        };

        // Derive the solana tenant PDA to scope user queries.
        let (solana_tenant_pk, _) = get_tenant_pda(&program_id, "solana");
        let default_tenant_pk = Pubkey::default();

        eprintln!("Fetching DZ Ledger users and validator metadata...");
        let (all_users_unfiltered, validators) = tokio::try_join!(
            async {
                dz_ledger
                    .fetch_all_dz_users()
                    .await
                    .map_err(|e| eyre::eyre!(e))
            },
            async {
                validator_metadata
                    .fetch_validators()
                    .await
                    .map_err(|e| eyre::eyre!(e))
            },
        )?;

        // Scope to solana tenant (or default/unset tenant).
        let all_users: Vec<_> = all_users_unfiltered
            .into_iter()
            .filter(|u| u.tenant_pk == solana_tenant_pk || u.tenant_pk == default_tenant_pk)
            .collect();

        let ibrl_users: Vec<_> = all_users
            .iter()
            .filter(|u| {
                u.user_type == UserType::IBRL || u.user_type == UserType::IBRLWithAllocatedIP
            })
            .collect();
        let ibrl_ips: std::collections::HashSet<Ipv4Addr> =
            ibrl_users.iter().map(|u| u.client_ip).collect();

        // Build per-IP set of multicast groups the IP publishes to.
        let mut publisher_groups_by_ip: HashMap<Ipv4Addr, std::collections::HashSet<Pubkey>> =
            HashMap::new();
        for u in all_users
            .iter()
            .filter(|u| u.user_type == UserType::Multicast)
        {
            for pk in &u.publishers {
                publisher_groups_by_ip
                    .entry(u.client_ip)
                    .or_default()
                    .insert(*pk);
            }
        }

        // User type breakdown.
        let ibrl_count = all_users
            .iter()
            .filter(|u| u.user_type == UserType::IBRL)
            .count();
        let ibrl_ip_count = all_users
            .iter()
            .filter(|u| u.user_type == UserType::IBRLWithAllocatedIP)
            .count();
        let multicast_count = all_users
            .iter()
            .filter(|u| u.user_type == UserType::Multicast)
            .count();
        let edge_count = all_users
            .iter()
            .filter(|u| u.user_type == UserType::EdgeFiltering)
            .count();
        let other_count =
            all_users.len() - ibrl_count - ibrl_ip_count - multicast_count - edge_count;

        // "On DZ" = validator's gossip IP matches an IBRL user's client IP.
        let dz_validator_count = validators.keys().filter(|ip| ibrl_ips.contains(ip)).count();
        let not_dz_count = validators.len() - dz_validator_count;
        eprintln!(
            "User accounts: {} total ({} IBRL, {} IBRL+IP, {} Multicast, {} EdgeFiltering{})",
            all_users.len(),
            ibrl_count,
            ibrl_ip_count,
            multicast_count,
            edge_count,
            if other_count > 0 {
                format!(", {} other", other_count)
            } else {
                String::new()
            },
        );
        eprintln!(
            "IBRL users: {} | Validators: {} ({} on DZ, {} not on DZ)",
            ibrl_users.len(),
            validators.len(),
            dz_validator_count,
            not_dz_count,
        );

        let filters = FindFilters {
            min_stake: self.min_stake,
            max_stake: self.max_stake,
            clients: self.client.clone(),
            is_publisher: self.is_publisher,
            not_publisher: self.not_publisher,
        };

        // Cross-reference IBRL users with validators by IP.
        let mut rows: Vec<ValidatorPublisherRow> = Vec::new();

        for user in &ibrl_users {
            if let Some(val) = validators.get(&user.client_ip) {
                let is_pub = publisher_groups_by_ip
                    .get(&user.client_ip)
                    .is_some_and(|groups| match &multicast_group_pk {
                        Some(group) => groups.contains(group),
                        None => !groups.is_empty(),
                    });

                if !apply_filters(&filters, val, is_pub) {
                    continue;
                }

                let device_label = device_infos
                    .get(&user.device_pk)
                    .map(|d| d.code.clone())
                    .unwrap_or_else(|| user.device_pk.to_string());

                let nearest_device_label = find_nearest_device_for_multicast(
                    &user.device_pk,
                    &device_infos,
                    latency_map.as_ref(),
                )
                .map(|d| d.code.clone())
                .unwrap_or_default();

                rows.push(ValidatorPublisherRow {
                    owner: user.owner.to_string(),
                    client_ip: user.client_ip.to_string(),
                    device: device_label,
                    nearest_device: nearest_device_label,
                    vote_account: val.vote_account.clone(),
                    stake_sol: format!("{:.2}", val.activated_stake_sol),
                    client: val.software_client.clone(),
                    version: val.software_version.clone(),
                    is_publisher: if is_pub { "yes" } else { "no" }.to_string(),
                });
            }
        }

        // Include validators not on DZ for summary or when explicitly requested.
        if self.include_not_on_dz || self.summary {
            for val in validators.values() {
                if ibrl_ips.contains(&val.gossip_ip) {
                    continue; // already included above
                }

                if !apply_filters(&filters, val, false) {
                    continue;
                }

                rows.push(ValidatorPublisherRow {
                    owner: String::new(),
                    client_ip: val.gossip_ip.to_string(),
                    device: String::new(),
                    nearest_device: String::new(),
                    vote_account: val.vote_account.clone(),
                    stake_sol: format!("{:.2}", val.activated_stake_sol),
                    client: val.software_client.clone(),
                    version: val.software_version.clone(),
                    is_publisher: "no".to_string(),
                });
            }
        }

        // Sort by stake descending.
        rows.sort_by(|a, b| {
            let sa: f64 = a.stake_sol.parse().unwrap_or(0.0);
            let sb: f64 = b.stake_sol.parse().unwrap_or(0.0);
            sb.partial_cmp(&sa).unwrap_or(std::cmp::Ordering::Equal)
        });

        if self.summary {
            // (count, on_dz, publishers)
            let mut by_client: HashMap<String, (usize, usize, usize)> = HashMap::new();
            for row in &rows {
                let entry = by_client.entry(row.client.clone()).or_insert((0, 0, 0));
                entry.0 += 1;
                if !row.owner.is_empty() {
                    entry.1 += 1; // on DZ
                }
                if row.is_publisher == "yes" {
                    entry.2 += 1;
                }
            }

            let total = rows.len();
            let total_on_dz = rows.iter().filter(|r| !r.owner.is_empty()).count();
            let total_pubs = rows.iter().filter(|r| r.is_publisher == "yes").count();

            let mut summary_rows: Vec<SummaryRow> = by_client
                .into_iter()
                .map(|(client, (count, on_dz, pubs))| SummaryRow {
                    client,
                    validators: count,
                    on_dz,
                    not_on_dz: count - on_dz,
                    publishers: pubs,
                    not_publishers: on_dz - pubs,
                })
                .collect();
            summary_rows.sort_by(|a, b| b.validators.cmp(&a.validators));
            summary_rows.push(SummaryRow {
                client: "TOTAL".to_string(),
                validators: total,
                on_dz: total_on_dz,
                not_on_dz: total - total_on_dz,
                publishers: total_pubs,
                not_publishers: total_on_dz - total_pubs,
            });
            print_table(summary_rows, &self.output, &[1, 2, 3, 4, 5]);
        } else {
            if rows.is_empty() {
                if self.output.json {
                    println!("[]");
                } else {
                    eprintln!("No IBRL validators found matching filters.");
                }
                return Ok(());
            }

            if !self.output.json {
                eprintln!("\nFound {} IBRL validator(s)\n", rows.len());
            }

            // right-align: STAKE (SOL) is column index 5
            print_table(rows, &self.output, &[5]);
        }

        Ok(())
    }
}

// ---------------------------------------------------------------------------
// Create command
// ---------------------------------------------------------------------------

/// Create multicast publisher users for IBRL validators that don't have one yet.
#[derive(Debug, Args)]
pub struct CreateValidatorMulticastPublishersCommand {
    /// Multicast group (pubkey or code, e.g. "edge-solana-shreds"). Required.
    #[arg(long, value_name = "KEY_OR_CODE")]
    multicast_group: String,

    /// Maximum number of users to create in this run.
    #[arg(long, value_name = "N")]
    limit: Option<usize>,

    /// Minimum activated stake in SOL to include.
    #[arg(long, value_name = "SOL")]
    min_stake: Option<f64>,

    /// Maximum activated stake in SOL to include.
    #[arg(long, value_name = "SOL")]
    max_stake: Option<f64>,

    /// Filter by validator client name (e.g. "JitoLabs", "AgaveBam", "Frankendancer"). Repeatable.
    #[arg(long, value_name = "NAME")]
    client: Vec<String>,

    /// Filter by client IP address. Repeatable.
    #[arg(long, value_name = "IP")]
    ip: Vec<Ipv4Addr>,

    /// Validator metadata service URL.
    #[arg(long, value_name = "URL", default_value = DEFAULT_VALIDATOR_METADATA_URL)]
    validator_metadata_url: String,

    /// Use geographic distance (Haversine) instead of onchain link latency for nearest-device.
    #[arg(long)]
    nearest_via_geo: bool,

    /// Simulate transactions without sending.
    #[arg(long)]
    dry_run: bool,
}

impl CreateValidatorMulticastPublishersCommand {
    pub async fn execute(self, dzclient: &DZClient) -> eyre::Result<()> {
        let program_id = *dzclient.get_program_id();
        let rpc_client = dzclient.rpc_client();
        let payer = dzclient
            .payer_keypair()
            .ok_or_else(|| eyre::eyre!("No keypair configured. Use --keypair to specify one."))?;
        let payer_pk = payer.pubkey();

        let mut device_infos: HashMap<Pubkey, DzDeviceInfo> =
            dz_ledger_reader::fetch_device_infos(rpc_client, &program_id).unwrap_or_default();

        let latency_map = if self.nearest_via_geo {
            None
        } else {
            let telemetry_id = dzclient
                .get_environment()
                .config()
                .ok()
                .map(|c| c.telemetry_program_id);
            telemetry_id.and_then(|tid| {
                dz_ledger_reader::fetch_device_latency_map(rpc_client, &tid)
                    .map_err(|e| eprintln!("Warning: could not fetch latency data: {e}"))
                    .ok()
            })
        };

        let validator_metadata = HttpValidatorMetadataReader {
            api_url: self.validator_metadata_url.clone(),
        };
        let dz_ledger = RpcDzLedgerReader::new(
            NonblockingRpcClient::new_with_commitment(
                dzclient.get_rpc().clone(),
                CommitmentConfig::confirmed(),
            ),
            program_id,
        );

        // Resolve multicast group.
        let multicast_group_pk =
            dz_ledger_reader::resolve_multicast_group(&self.multicast_group, &dz_ledger)
                .await
                .map_err(|e| eyre::eyre!(e))?;
        eprintln!(
            "Multicast group: {} ({})",
            multicast_group_pk, self.multicast_group
        );

        // Derive the solana tenant PDA to scope user queries.
        let (solana_tenant_pk, _) = get_tenant_pda(&program_id, "solana");
        let default_tenant_pk = Pubkey::default();

        // Fetch users and validator data.
        eprintln!("Fetching DZ Ledger users and validator metadata...");
        let (all_users_unfiltered, validators) = tokio::try_join!(
            async {
                dz_ledger
                    .fetch_all_dz_users()
                    .await
                    .map_err(|e| eyre::eyre!(e))
            },
            async {
                validator_metadata
                    .fetch_validators()
                    .await
                    .map_err(|e| eyre::eyre!(e))
            },
        )?;

        // Scope to solana tenant (or default/unset tenant).
        let all_users: Vec<_> = all_users_unfiltered
            .into_iter()
            .filter(|u| u.tenant_pk == solana_tenant_pk || u.tenant_pk == default_tenant_pk)
            .collect();

        let filters = CandidateFilters {
            min_stake: self.min_stake,
            max_stake: self.max_stake,
            clients: self.client,
            ips: self.ip.clone(),
            limit: self.limit,
        };

        let candidates = find_candidates(
            &all_users,
            &validators,
            &multicast_group_pk,
            &filters,
            &device_infos,
        );

        if candidates.is_empty() {
            // If specific IPs were requested, explain why each was skipped.
            if !self.ip.is_empty() {
                let ibrl_ips: std::collections::HashSet<Ipv4Addr> = all_users
                    .iter()
                    .filter(|u| {
                        u.user_type == doublezero_sdk::UserType::IBRL
                            || u.user_type == doublezero_sdk::UserType::IBRLWithAllocatedIP
                    })
                    .map(|u| u.client_ip)
                    .collect();
                let publisher_ips: std::collections::HashSet<Ipv4Addr> = all_users
                    .iter()
                    .filter(|u| {
                        u.user_type == doublezero_sdk::UserType::Multicast
                            && u.publishers.contains(&multicast_group_pk)
                    })
                    .map(|u| u.client_ip)
                    .collect();
                eprintln!("No candidates found. Per-IP diagnosis:");
                for ip in &self.ip {
                    let reason: String = if !ibrl_ips.contains(ip) {
                        "no IBRL user found for this IP".to_string()
                    } else if publisher_ips.contains(ip) {
                        "already a publisher for this multicast group".to_string()
                    } else if !validators.contains_key(ip) {
                        "not found in validator metadata service".to_string()
                    } else {
                        let val = validators.get(ip).unwrap();
                        let client_mismatch = !filters.clients.is_empty() && {
                            let name = val.software_client.to_lowercase();
                            !filters
                                .clients
                                .iter()
                                .any(|c| name.contains(&c.to_lowercase()))
                        };
                        let stake_mismatch = filters
                            .min_stake
                            .is_some_and(|m| val.activated_stake_sol < m)
                            || filters
                                .max_stake
                                .is_some_and(|m| val.activated_stake_sol > m);
                        if client_mismatch {
                            format!(
                                "client '{}' does not match --client filter {:?}",
                                val.software_client, filters.clients
                            )
                        } else if stake_mismatch {
                            "filtered out by stake filter".to_string()
                        } else {
                            "filtered out (unknown reason)".to_string()
                        }
                    };
                    eprintln!("  {ip}: {reason}");
                }
            } else {
                eprintln!(
                    "No candidates found — all matching validators already have a publisher."
                );
            }
            return Ok(());
        }

        // Display plan (snapshot — target devices are re-evaluated at execution time).
        eprintln!(
            "\nWill create {} multicast publisher user(s) on group {}:\n",
            candidates.len(),
            self.multicast_group,
        );
        #[derive(Tabled, Serialize)]
        struct PlanRow {
            #[tabled(rename = "OWNER")]
            owner: String,
            #[tabled(rename = "CLIENT IP")]
            client_ip: String,
            #[tabled(rename = "DEVICE")]
            device: String,
            #[tabled(rename = "NEAREST DEVICE")]
            nearest_device: String,
            #[tabled(rename = "CLIENT")]
            client: String,
            #[tabled(rename = "STAKE (SOL)")]
            stake_sol: String,
        }

        let plan_rows: Vec<PlanRow> = candidates
            .iter()
            .map(|c| {
                let nearest = find_nearest_device_for_multicast(
                    &c.device_pk,
                    &device_infos,
                    latency_map.as_ref(),
                )
                .map(|d| d.code.clone())
                .unwrap_or_else(|| "none".to_string());
                PlanRow {
                    owner: c.owner.to_string(),
                    client_ip: c.client_ip.to_string(),
                    device: c.device_label.clone(),
                    nearest_device: nearest,
                    client: c.software_client.clone(),
                    stake_sol: format!("{:.2}", c.stake_sol),
                }
            })
            .collect();
        print_table(plan_rows, &OutputOptions { json: false }, &[5]);
        eprintln!();

        if self.dry_run {
            eprintln!("Dry run — no transactions sent.");
            return Ok(());
        }

        // Confirmation prompt.
        eprint!("Proceed? [y/N] ");
        std::io::stderr().flush()?;
        let mut input = String::new();
        std::io::stdin().read_line(&mut input)?;
        let input = input.trim().to_lowercase();
        if input != "y" && input != "yes" {
            eyre::bail!("Aborted");
        }

        // Execute: for each candidate, re-evaluate the target device at execution time so
        // that slots filled earlier in this run are reflected in subsequent picks.
        let mut created = 0;
        let mut skipped = 0;
        for (i, candidate) in candidates.iter().enumerate() {
            // Re-evaluate nearest available device now, accounting for slots filled this run.
            let target = find_nearest_device_for_multicast(
                &candidate.device_pk,
                &device_infos,
                latency_map.as_ref(),
            );
            let (target_device_pk, target_device_label) = match target {
                Some(d) => (d.pk, d.code.clone()),
                None => {
                    eprintln!(
                        "\n[{}/{}] Skipping {} (ip: {}) — no device with available capacity.",
                        i + 1,
                        candidates.len(),
                        candidate.owner,
                        candidate.client_ip,
                    );
                    skipped += 1;
                    continue;
                }
            };

            eprintln!(
                "\n[{}/{}] Creating multicast publisher for {} (ip: {}, device: {}, target: {})...",
                i + 1,
                candidates.len(),
                candidate.owner,
                candidate.client_ip,
                candidate.device_label,
                target_device_label,
            );

            let dz_user = DzUser {
                owner: candidate.owner,
                client_ip: candidate.client_ip,
                device_pk: target_device_pk,
                tenant_pk: Pubkey::default(),
                user_type: doublezero_sdk::UserType::IBRL,
                publishers: vec![],
            };

            let ixs = match build_create_multicast_publisher_instructions(
                &program_id,
                &payer_pk,
                &candidate.owner,
                &multicast_group_pk,
                &dz_user,
            ) {
                Ok(ixs) => ixs,
                Err(e) => {
                    eprintln!("  Error building instructions: {e} — skipping.");
                    skipped += 1;
                    continue;
                }
            };

            let result = async {
                send_instruction(rpc_client, payer, &[ixs.set_access_pass], "set_access_pass")
                    .await?;
                send_instruction(rpc_client, payer, &[ixs.add_allowlist], "add_pub_allowlist")
                    .await?;
                send_instruction(
                    rpc_client,
                    payer,
                    &[ixs.create_user],
                    "create_subscribe_user",
                )
                .await
            }
            .await;

            match result {
                Ok(()) => {
                    eprintln!(
                        "  Created multicast publisher for {} on {}",
                        candidate.client_ip, target_device_label,
                    );
                    // Decrement local capacity so subsequent candidates see the updated state.
                    if let Some(d) = device_infos.get_mut(&target_device_pk) {
                        d.users_count += 1;
                        d.multicast_publishers_count += 1;
                    }
                    created += 1;
                }
                Err(e) => {
                    eprintln!("  Error: {e} — skipping.");
                    skipped += 1;
                }
            }
        }

        eprintln!(
            "\nDone — created {created} multicast publisher(s){}.",
            if skipped > 0 {
                format!(", skipped {skipped}")
            } else {
                String::new()
            }
        );

        Ok(())
    }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

async fn send_instruction(
    rpc_client: &RpcClient,
    payer: &Keypair,
    ixs: &[Instruction],
    label: &str,
) -> eyre::Result<()> {
    let blockhash = rpc_client
        .get_latest_blockhash()
        .map_err(|e| eyre::eyre!("failed to get blockhash for {label}: {e}"))?;

    let mut tx = Transaction::new_with_payer(ixs, Some(&payer.pubkey()));
    tx.sign(&[payer], blockhash);

    let sig = rpc_client
        .send_and_confirm_transaction(&tx)
        .map_err(|e| eyre::eyre!("failed to send {label}: {e}"))?;

    eprintln!("  {label}: {sig}");

    Ok(())
}
