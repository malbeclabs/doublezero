use std::path::PathBuf;

use anyhow::{Result, ensure};
use clap::Subcommand;
use doublezero_solana_client_tools::rpc::SolanaConnection;
use doublezero_solana_sdk::revenue_distribution::fetch::{
    SolConversionState, try_fetch_config, try_fetch_distribution,
};
use slack_notifier::contributor_rewards::{
    DistributionRewardRow, WriteResultInfo, post_contributor_rewards, post_distribution_rewards,
};
use solana_sdk::pubkey::Pubkey;
use tabled::{builder::Builder as TableBuilder, settings::Style};
use tracing::{info, warn};

use crate::{
    calculator::{ledger_operations::WriteResult, orchestrator::Orchestrator},
    cli::snapshot::CompleteSnapshot,
};

/// Reward-related commands
#[derive(Subcommand, Debug)]
pub enum RewardsCommands {
    #[command(
        about = "Calculate Shapley value-based rewards for network contributors",
        after_help = r#"Examples:
    # Calculate rewards from snapshot
    calculate-rewards --snapshot mn-epoch-27-snapshot.json -k keypair.json

    # Dry run to preview without writing to DZ ledger
    calculate-rewards --snapshot mn-epoch-27-snapshot.json --dry-run

    # Skip only device telemetry write (write everything else)
    calculate-rewards --snapshot mn-epoch-27-snapshot.json -k keypair.json --skip-device-telemetry

    # Skip multiple writes (e.g., skip telemetry but write rewards)
    calculate-rewards --snapshot mn-epoch-27-snapshot.json -k keypair.json --skip-device-telemetry --skip-internet-telemetry

    # Repost only merkle root (skip all DZ Ledger writes)
    calculate-rewards --snapshot mn-epoch-27-snapshot.json -k keypair.json --skip-device-telemetry --skip-internet-telemetry --skip-reward-input --skip-shapley-output

    # Create a snapshot first using the snapshot command
    snapshot all --epoch 27 --output-file mn-epoch-27-snapshot.json"#
    )]
    CalculateRewards {
        /// Path to epoch snapshot file (REQUIRED for reproducible calculations)
        #[arg(
            short = 's',
            long,
            value_name = "FILE",
            help = "Snapshot file containing epoch data"
        )]
        snapshot: PathBuf,

        /// Skip writing to ledger and show what would be written
        #[arg(long)]
        dry_run: bool,

        /// Path to keypair file for signing transactions
        #[arg(
            short = 'k',
            long,
            value_name = "FILE",
            required_unless_present = "dry_run"
        )]
        keypair: Option<PathBuf>,

        /// Skip writing device telemetry aggregates to DZ Ledger
        #[arg(long)]
        skip_device_telemetry: bool,

        /// Skip writing internet telemetry aggregates to DZ Ledger
        #[arg(long)]
        skip_internet_telemetry: bool,

        /// Skip writing reward calculation input to DZ Ledger
        #[arg(long)]
        skip_reward_input: bool,

        /// Skip writing shapley output storage to DZ Ledger
        #[arg(long)]
        skip_shapley_output: bool,

        /// Skip posting merkle root to Solana
        #[arg(long)]
        skip_merkle_root: bool,

        /// Send Slack notification after completion (requires Slack settings in config)
        #[arg(long)]
        slack_notify: bool,
    },
    #[command(
        about = "Read and display telemetry aggregate statistics from the ledger",
        after_help = r#"Examples:
    # Read all telemetry for epoch 123
    read-telem-agg --epoch 123

    # Export device telemetry to CSV
    read-telem-agg --epoch 123 --type device -o device_stats.csv

    # Read internet telemetry only
    read-telem-agg --epoch 123 --type internet"#
    )]
    ReadTelemAgg {
        /// DZ epoch number to read telemetry from
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Rewards accountant public key (auto-fetched from ProgramConfig if not provided)
        #[arg(short = 'r', long, value_name = "PUBKEY")]
        rewards_accountant: Option<Pubkey>,

        /// Type of telemetry to read: 'device', 'internet', or 'all'
        #[arg(short = 't', long, default_value = "all", value_name = "TYPE")]
        r#type: String,

        /// Export results to CSV file
        #[arg(short = 'o', long, value_name = "FILE")]
        output_csv: Option<PathBuf>,
    },
    #[command(
        about = "Check and verify a specific contributor's reward for an epoch",
        after_help = r#"Examples:
    # Check reward for a contributor
    check-reward --contributor 7EcDhSYGxXyscszYEp35KHN8vvw3svAuLKTzXwCFLtV --epoch 123

    # Check with explicit rewards accountant
    check-reward -c 7EcDhSYGxXyscszYEp35KHN8vvw3svAuLKTzXwCFLtV -e 123 -r <ACCOUNTANT_PUBKEY>

    # Output as JSON
    check-reward -c 7EcDhSYGxXyscszYEp35KHN8vvw3svAuLKTzXwCFLtV -e 123 --json"#
    )]
    CheckReward {
        /// Contributor's public key (base58 encoded)
        #[arg(short, long, value_name = "PUBKEY")]
        contributor: Pubkey,

        /// DZ epoch number to check reward for
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Rewards accountant public key (auto-fetched from ProgramConfig if not provided)
        #[arg(short = 'r', long, value_name = "PUBKEY")]
        rewards_accountant: Option<Pubkey>,

        /// Output as JSON instead of table
        #[arg(long)]
        json: bool,
    },
    #[command(
        about = "Read and display the reward input configuration for an epoch",
        after_help = r#"Examples:
    # Read reward input for epoch 123
    read-reward-input --epoch 123

    # Read with specific rewards accountant
    read-reward-input --epoch 123 --rewards-accountant <PUBKEY>"#
    )]
    ReadRewardInput {
        /// DZ epoch number to read configuration from
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Rewards accountant public key (auto-fetched from ProgramConfig if not provided)
        #[arg(short = 'r', long, value_name = "PUBKEY")]
        rewards_accountant: Option<Pubkey>,
    },
    #[command(
        about = "Read and display all contributor rewards for an epoch",
        after_help = r#"Examples:
    # Read all rewards for epoch 56
    read-rewards --epoch 56

    # Read with JSON output
    read-rewards --epoch 56 --json

    # Read with specific rewards accountant
    read-rewards --epoch 56 --rewards-accountant <PUBKEY>"#
    )]
    ReadRewards {
        /// DZ epoch number to read rewards from
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Rewards accountant public key (auto-fetched from ProgramConfig if not provided)
        #[arg(short = 'r', long, value_name = "PUBKEY")]
        rewards_accountant: Option<Pubkey>,

        /// Output as JSON instead of table
        #[arg(long)]
        json: bool,
    },
    #[command(
        about = "Reallocate a record account to change its size",
        after_help = r#"Examples:
    # Increase device telemetry record size
    realloc-record --type device-telemetry --epoch 123 --size 100000 -k keypair.json

    # Dry run to check the operation
    realloc-record --type internet-telemetry --epoch 123 --size 50000 --dry-run"#
    )]
    ReallocRecord {
        /// Record type: 'device-telemetry', 'internet-telemetry', 'reward-input', or 'contributor-rewards'
        #[arg(short = 't', long, value_name = "TYPE")]
        r#type: String,

        /// DZ epoch number of the record to reallocate
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// New size in bytes for the record account
        #[arg(short, long, value_name = "BYTES")]
        size: u64,

        /// Skip the actual reallocation and show what would happen
        #[arg(long)]
        dry_run: bool,

        /// Path to keypair file for signing transactions
        #[arg(
            short = 'k',
            long,
            value_name = "FILE",
            required_unless_present = "dry_run"
        )]
        keypair: Option<PathBuf>,
    },
    #[command(
        about = "Close a record account and reclaim its rent",
        after_help = r#"Examples:
    # Close an old telemetry record
    close-record --type device-telemetry --epoch 100 -k keypair.json

    # Dry run to verify the account exists
    close-record --type contributor-rewards --epoch 100 --dry-run"#
    )]
    CloseRecord {
        /// Record type: 'device-telemetry', 'internet-telemetry', 'reward-input', or 'contributor-rewards'
        #[arg(short = 't', long, value_name = "TYPE")]
        r#type: String,

        /// DZ epoch number of the record to close
        #[arg(short, long, value_name = "EPOCH")]
        epoch: u64,

        /// Skip the actual closure and show what would happen
        #[arg(long)]
        dry_run: bool,

        /// Path to keypair file for signing transactions
        #[arg(
            short = 'k',
            long,
            value_name = "FILE",
            required_unless_present = "dry_run"
        )]
        keypair: Option<PathBuf>,
    },
    #[command(
        about = "Write telemetry aggregate statistics to the ledger without calculating rewards",
        after_help = r#"Examples:
    # Write all telemetry for previous epoch
    write-telem-agg -k keypair.json

    # Write only device telemetry for epoch 123
    write-telem-agg --epoch 123 --type device -k keypair.json

    # Dry run to preview the data
    write-telem-agg --epoch 123 --dry-run"#
    )]
    WriteTelemAgg {
        /// DZ epoch to process telemetry for (defaults to previous epoch)
        #[arg(short, long, value_name = "EPOCH")]
        epoch: Option<u64>,

        /// Skip writing to ledger and show what would be written
        #[arg(long)]
        dry_run: bool,

        /// Type of telemetry to write: 'device', 'internet', or 'all'
        #[arg(short = 't', long, default_value = "all", value_name = "TYPE")]
        r#type: String,

        /// Path to keypair file for signing transactions
        #[arg(
            short = 'k',
            long,
            value_name = "FILE",
            required_unless_present = "dry_run"
        )]
        keypair: Option<PathBuf>,
    },
    #[command(
        about = "Distribute rewards to contributors for a finalized epoch",
        long_about = "Three modes of operation:\n\n\
            1. Readiness check (--dry-run, no keypair): shows distribution status without simulation\n\
            2. Simulate (--dry-run with keypair): simulates transactions without sending\n\
            3. Execute (no --dry-run, keypair required): sends real transactions",
        after_help = r#"Examples:
    # Check distribution readiness (no keypair needed)
    distribute-rewards --dry-run

    # Check a specific epoch
    distribute-rewards --dry-run -e 27

    # Simulate distribution (dry-run with keypair)
    distribute-rewards --dry-run -k keypair.json -e 27

    # Execute distribution
    distribute-rewards -k keypair.json -e 27"#
    )]
    DistributeRewards {
        /// DZ epoch to distribute rewards for (defaults to current eligible epoch)
        #[arg(short = 'e', long, value_name = "EPOCH")]
        dz_epoch: Option<u64>,

        /// Skip sending transactions and show what would happen
        #[arg(long)]
        dry_run: bool,

        /// Path to keypair file for signing transactions
        #[arg(
            short = 'k',
            long,
            value_name = "FILE",
            required_unless_present = "dry_run"
        )]
        keypair: Option<PathBuf>,
    },
}

/// Handle rewards commands
pub async fn handle(orchestrator: &Orchestrator, cmd: RewardsCommands) -> Result<()> {
    match cmd {
        RewardsCommands::CalculateRewards {
            snapshot,
            dry_run,
            keypair,
            skip_device_telemetry,
            skip_internet_telemetry,
            skip_reward_input,
            skip_shapley_output,
            skip_merkle_root,
            slack_notify,
        } => {
            use tracing::warn;

            // Construct WriteConfig from CLI flags
            let write_config = crate::calculator::WriteConfig::from_flags(
                skip_device_telemetry,
                skip_internet_telemetry,
                skip_reward_input,
                skip_shapley_output,
                skip_merkle_root,
            );

            // Validation: Warn if dry-run is combined with skip flags
            if dry_run && write_config.skipped_count() > 0 {
                warn!(
                    "Both --dry-run and skip flags specified. --dry-run takes precedence and all writes will be skipped."
                );
            }

            // Validation: Warn if all skip flags are set (suggest using --dry-run instead)
            if !dry_run && write_config.all_writes_skipped() {
                warn!(
                    "All write operations are skipped via flags. Consider using --dry-run instead for clearer intent."
                );
            }

            // Validation: Require keypair if not dry-run and any writes are enabled
            if !dry_run && write_config.any_writes_enabled() && keypair.is_none() {
                anyhow::bail!(
                    "Keypair is required when write operations are enabled. \
                     Provide --keypair or use --dry-run to skip all writes."
                );
            }

            let write_summary = orchestrator
                .calculate_rewards(None, keypair, Some(snapshot.clone()), dry_run, write_config)
                .await?;

            // Send Slack notification if requested
            if slack_notify {
                // Load snapshot to get epoch
                let snapshot_data = CompleteSnapshot::load_from_file(&snapshot)?;
                let epoch = snapshot_data.dz_epoch;

                // Check if Slack is configured
                if let Some(slack_settings) = &orchestrator.settings.slack {
                    if let Some(webhook_url) = &slack_settings.webhook_url {
                        let network = format!("{:?}", orchestrator.settings.network);

                        // Convert WriteSummary to WriteResultInfo
                        let write_results: Vec<WriteResultInfo> = write_summary
                            .results
                            .iter()
                            .map(|result| match result {
                                WriteResult::Success(description, identifier) => {
                                    WriteResultInfo::Success {
                                        description: description.clone(),
                                        identifier: identifier.clone(),
                                    }
                                }
                                WriteResult::Failed(description, error) => {
                                    WriteResultInfo::Failed {
                                        description: description.clone(),
                                        error: error.clone(),
                                    }
                                }
                            })
                            .collect();

                        // Post notification
                        match post_contributor_rewards(webhook_url, network, epoch, write_results)
                            .await
                        {
                            Ok(_) => {
                                info!("[OK] Posted Slack notification for epoch {}", epoch);
                            }
                            Err(e) => {
                                warn!("[WARN] Failed to post Slack notification: {}", e);
                            }
                        }
                    } else {
                        warn!("[WARN] Slack notification requested but webhook_url not configured");
                    }
                } else {
                    warn!("[WARN] Slack notification requested but Slack settings not configured");
                }
            }

            Ok(())
        }
        RewardsCommands::ReadTelemAgg {
            epoch,
            rewards_accountant,
            r#type,
            output_csv,
        } => {
            orchestrator
                .read_telemetry_aggregates(epoch, rewards_accountant, &r#type, output_csv)
                .await
        }
        RewardsCommands::CheckReward {
            contributor,
            epoch,
            rewards_accountant,
            json,
        } => {
            orchestrator
                .check_contributor_reward(&contributor, epoch, rewards_accountant, json)
                .await
        }
        RewardsCommands::ReadRewardInput {
            epoch,
            rewards_accountant,
        } => {
            orchestrator
                .read_reward_input(epoch, rewards_accountant)
                .await
        }
        RewardsCommands::ReadRewards {
            epoch,
            rewards_accountant,
            json,
        } => {
            orchestrator
                .read_all_rewards(epoch, rewards_accountant, json)
                .await
        }
        RewardsCommands::ReallocRecord {
            r#type,
            epoch,
            size,
            dry_run,
            keypair,
        } => {
            orchestrator
                .realloc_record(r#type, epoch, size, keypair, dry_run)
                .await
        }
        RewardsCommands::CloseRecord {
            r#type,
            epoch,
            dry_run,
            keypair,
        } => {
            orchestrator
                .close_record(r#type, epoch, keypair, dry_run)
                .await
        }
        RewardsCommands::WriteTelemAgg {
            epoch,
            dry_run,
            r#type,
            keypair,
        } => {
            orchestrator
                .write_telemetry_aggregates(epoch, keypair, dry_run, r#type)
                .await
        }
        RewardsCommands::DistributeRewards {
            dz_epoch,
            dry_run,
            keypair,
        } => {
            let connection =
                SolanaConnection::new(orchestrator.settings.rpc.solana_write_url.clone());

            let (_, config) = try_fetch_config(&connection).await?;

            let dz_epoch_value = match dz_epoch {
                Some(epoch) => {
                    info!("Will distribute for provided dz_epoch: {epoch}");
                    epoch
                }
                None => {
                    let sol_conversion_state = SolConversionState::try_fetch(&connection).await?;
                    let next_sweep = sol_conversion_state
                        .journal
                        .1
                        .next_dz_epoch_to_sweep_tokens
                        .value();
                    ensure!(next_sweep > 0, "No epochs have been swept yet");
                    let dist_epoch = next_sweep - 1;
                    info!("Will distribute for dz_epoch: {dist_epoch}, next_sweep: {next_sweep}");
                    dist_epoch
                }
            };

            match (dry_run, keypair) {
                // Mode 1: Readiness check only (no keypair, no wallet)
                (true, None) => {
                    let (_, distribution) =
                        try_fetch_distribution(&connection, dz_epoch_value).await?;
                    info!("Epoch {dz_epoch_value} readiness:");
                    info!(
                        "  Rewards finalized: {}",
                        distribution.is_rewards_calculation_finalized()
                    );
                    info!("  2Z tokens swept: {}", distribution.has_swept_2z_tokens());
                    info!(
                        "  Progress: {}/{}",
                        distribution.distributed_rewards_count, distribution.total_contributors
                    );
                }
                // Mode 2 & 3: Simulate or Execute (keypair present)
                (dry_run, Some(keypair_path)) => {
                    use doublezero_solana_client_tools::{
                        payer::Wallet, rpc::DoubleZeroLedgerConnection,
                    };

                    use crate::calculator::{distribute, keypair_loader::load_keypair};

                    let signer = load_keypair(&Some(keypair_path))?;
                    let dz_connection =
                        DoubleZeroLedgerConnection::new(orchestrator.settings.rpc.dz_url.clone());

                    let wallet = Wallet {
                        connection,
                        signer,
                        compute_unit_price_ix: None,
                        verbose: false,
                        fee_payer: None,
                        dry_run,
                    };

                    info!("Distributing rewards for epoch {dz_epoch_value}");

                    let shapley_prefix = orchestrator.settings.get_contributor_rewards_prefix();

                    let summary = distribute::try_distribute_epoch_rewards(
                        &wallet,
                        &dz_connection,
                        &config.rewards_accountant_key,
                        dz_epoch_value,
                        &shapley_prefix,
                    )
                    .await?;

                    match &summary.outcome {
                        distribute::DistributionOutcome::Complete { total_contributors } => {
                            info!(
                                "Epoch {dz_epoch_value} complete: {total_contributors}/{total_contributors} distributed"
                            );
                        }
                        distribute::DistributionOutcome::PartiallyComplete {
                            total_contributors,
                            distributed,
                            skipped,
                        } => {
                            info!(
                                "Epoch {dz_epoch_value} partially complete: {distributed}/{total_contributors} distributed, {skipped} skipped (missing ContributorRewards accounts)"
                            );
                        }
                        distribute::DistributionOutcome::NotReady => {
                            info!("Distribution not ready for epoch {dz_epoch_value}");
                        }
                    }

                    // Fetch contributor labels and build display rows.
                    if !summary.contributors.is_empty() {
                        let labels =
                            crate::calculator::ledger_operations::try_fetch_contributor_labels(
                                &dz_connection,
                                &orchestrator.settings.programs.serviceability_program_id,
                            )
                            .await
                            .unwrap_or_default();

                        let resolve_label = |key: &solana_sdk::pubkey::Pubkey| {
                            labels.get(key).cloned().unwrap_or_else(|| key.to_string())
                        };

                        // Print per-contributor rewards table to stdout.
                        let mut table_builder = TableBuilder::default();
                        table_builder.push_record([
                            "dz_epoch".to_string(),
                            "index".to_string(),
                            "contributor".to_string(),
                            "proportion".to_string(),
                            "reward".to_string(),
                            "distributed".to_string(),
                        ]);
                        for c in &summary.contributors {
                            table_builder.push_record([
                                summary.dz_epoch.to_string(),
                                c.index.to_string(),
                                resolve_label(&c.contributor_key),
                                format!("{:.2}%", 100.0 * c.proportion),
                                format!("{:.1} 2Z", c.reward_tokens),
                                if c.distributed { "yes" } else { "no" }.to_string(),
                            ]);
                        }
                        let table = table_builder
                            .build()
                            .with(Style::psql().remove_horizontals())
                            .to_string();
                        println!("\n{table}");

                        // Post Slack notification for completed distributions.
                        if matches!(
                            summary.outcome,
                            distribute::DistributionOutcome::Complete { .. }
                                | distribute::DistributionOutcome::PartiallyComplete { .. }
                        ) && let Some(slack_settings) = &orchestrator.settings.slack
                            && slack_settings.enabled
                            && let Some(webhook_url) = &slack_settings.webhook_url
                        {
                            let network = format!("{:?}", orchestrator.settings.network);
                            let rows: Vec<DistributionRewardRow> = summary
                                .contributors
                                .iter()
                                .map(|c| DistributionRewardRow {
                                    index: c.index,
                                    contributor: resolve_label(&c.contributor_key),
                                    proportion: format!("{:.2}%", 100.0 * c.proportion),
                                    reward: format!("{:.1} 2Z", c.reward_tokens),
                                    distributed: if c.distributed { "yes" } else { "no" }
                                        .to_string(),
                                })
                                .collect();

                            match post_distribution_rewards(
                                webhook_url,
                                network,
                                summary.dz_epoch,
                                rows,
                            )
                            .await
                            {
                                Ok(()) => {
                                    info!(
                                        "[OK] Posted distribution Slack notification for epoch {}",
                                        dz_epoch_value
                                    );
                                }
                                Err(e) => {
                                    warn!(
                                        "[WARN] Failed to post distribution Slack notification: {}",
                                        e
                                    );
                                }
                            }
                        }
                    }
                }
                // Unreachable: clap enforces keypair is required unless dry_run
                (false, None) => unreachable!(),
            }

            Ok(())
        }
    }
}
