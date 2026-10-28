use std::{path::PathBuf, sync::Mutex};

use anyhow::{Result, ensure};
use chrono::DateTime;
use clap::Subcommand;
use doublezero_solana_client_tools::{
    payer::Wallet,
    rpc::{DoubleZeroLedgerConnection, SolanaConnection},
};
use doublezero_solana_sdk::revenue_distribution::types::UnitShare32;
use slack_notifier::contributor_rewards::{WriteResultInfo, post_contributor_rewards};
use solana_sdk::pubkey::Pubkey;
use tabled::{builder::Builder as TableBuilder, settings::Style};
use tracing::{info, warn};

use crate::{
    calculator::{
        keypair_loader::load_keypair,
        ledger_operations::{
            RewardEntry, WriteResult, format_unit_share_proportion, try_fetch_contributor_labels,
        },
        monthly,
        monthly_distribute::{self, MonthAction, MonthlyContext},
        orchestrator::Orchestrator,
    },
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

    # Repost the merkle root from the shapley output already on the DZ Ledger (skips the Shapley calculation)
    calculate-rewards --snapshot mn-epoch-27-snapshot.json -k keypair.json --shapley-from-ledger

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

        /// Read the shapley output from the DZ Ledger instead of calculating it, and write no record to the DZ Ledger
        #[arg(long)]
        shapley_from_ledger: bool,

        /// Send Slack notification after completion (requires Slack settings in config)
        #[arg(long)]
        slack_notify: bool,
    },
    #[command(
        about = "Average a UTC month's posted epoch shares into monthly contributor shares",
        after_help = r#"Examples:
    # Calculate the shares for September 2026
    calculate-monthly-rewards --year 2026 --month 9

    # Output as JSON
    calculate-monthly-rewards --year 2026 --month 9 --json"#
    )]
    CalculateMonthlyRewards {
        /// UTC calendar year
        #[arg(long, value_name = "YEAR")]
        year: u16,

        /// UTC calendar month, 1 through 12
        #[arg(long, value_name = "MONTH", value_parser = clap::value_parser!(u8).range(1..=12))]
        month: u8,

        /// Output as JSON instead of tables
        #[arg(long)]
        json: bool,
    },
    #[command(
        about = "Finalize and distribute one month's contributor rewards by hand",
        after_help = r#"Examples:
    # Simulate finalizing and distributing September 2026
    distribute-monthly-rewards --year 2026 --month 9 --solana-url <SOLANA_URL> --dz-url <DZ_URL> -k keypair.json --dry-run

    # Finalize and distribute September 2026
    distribute-monthly-rewards --year 2026 --month 9 --solana-url <SOLANA_URL> --dz-url <DZ_URL> -k keypair.json"#
    )]
    DistributeMonthlyRewards {
        /// UTC calendar year
        #[arg(long, value_name = "YEAR")]
        year: u16,

        /// UTC calendar month, 1 through 12
        #[arg(long, value_name = "MONTH", value_parser = clap::value_parser!(u8).range(1..=12))]
        month: u8,

        /// Simulate transactions and write nothing
        #[arg(long)]
        dry_run: bool,

        /// Path to the rewards accountant's keypair file
        #[arg(short = 'k', long, value_name = "FILE")]
        keypair: Option<PathBuf>,

        /// Solana RPC URL. The command reads no config file
        #[arg(long, value_name = "URL")]
        solana_url: String,

        /// DoubleZero Ledger RPC URL
        #[arg(long, value_name = "URL")]
        dz_url: String,
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
            shapley_from_ledger,
            slack_notify,
        } => {
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
                .calculate_rewards(
                    None,
                    keypair,
                    Some(snapshot.clone()),
                    dry_run,
                    write_config,
                    shapley_from_ledger,
                )
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
        RewardsCommands::CalculateMonthlyRewards { year, month, json } => {
            let monthly_rewards = monthly::try_calculate_monthly_rewards(
                &monthly::MonthlySettings::from(&orchestrator.settings),
                year,
                month,
            )
            .await?;
            let total_units = monthly_rewards
                .rewards
                .iter()
                .map(|reward| u64::from(reward.unit_share))
                .sum::<u64>();

            if json {
                let rewards = monthly_rewards
                    .rewards
                    .iter()
                    .map(|reward| RewardEntry {
                        contributor: reward.contributor_key.to_string(),
                        unit_share: reward.unit_share,
                    })
                    .collect::<Vec<_>>();
                let output = serde_json::json!({
                    "year": year,
                    "month": month,
                    "epochs": monthly_rewards.epochs,
                    "total_units": total_units,
                    "rewards": rewards,
                });
                println!("{output}");

                return Ok(());
            }

            let dz_connection =
                DoubleZeroLedgerConnection::new(orchestrator.settings.rpc.dz_url.clone());
            let labels = try_fetch_contributor_labels(
                &dz_connection,
                &orchestrator.settings.programs.serviceability_program_id,
            )
            .await
            .unwrap_or_default();

            let mut epoch_table = TableBuilder::default();
            epoch_table.push_record(["dz_epoch".to_string(), "end_time".to_string()]);
            for epoch_end in &monthly_rewards.epochs {
                epoch_table.push_record([
                    epoch_end.epoch.to_string(),
                    DateTime::from_timestamp(epoch_end.end_time, 0).map_or_else(
                        || epoch_end.end_time.to_string(),
                        |end_time| end_time.to_rfc3339(),
                    ),
                ]);
            }

            let mut reward_table = TableBuilder::default();
            reward_table.push_record([
                "contributor".to_string(),
                "pubkey".to_string(),
                "unit_share".to_string(),
                "proportion".to_string(),
            ]);
            for reward in &monthly_rewards.rewards {
                reward_table.push_record([
                    labels
                        .get(&reward.contributor_key)
                        .cloned()
                        .unwrap_or_else(|| reward.contributor_key.to_string()),
                    reward.contributor_key.to_string(),
                    reward.unit_share.to_string(),
                    format_unit_share_proportion(reward.unit_share, u32::from(UnitShare32::MAX)),
                ]);
            }

            for table in [epoch_table, reward_table] {
                let table = table
                    .build()
                    .with(Style::psql().remove_horizontals())
                    .to_string();
                println!("{table}\n");
            }
            println!("Total units: {total_units}");

            Ok(())
        }
        RewardsCommands::DistributeMonthlyRewards {
            year,
            month,
            dry_run,
            keypair,
            solana_url,
            dz_url,
        } => distribute_monthly_rewards(year, month, dry_run, keypair, solana_url, dz_url).await,
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
    }
}

/// Finalize and distribute one month by hand. It reads nothing from the
/// settings, so the binary runs it without a config file.
pub async fn distribute_monthly_rewards(
    year: u16,
    month: u8,
    dry_run: bool,
    keypair: Option<PathBuf>,
    solana_url: String,
    dz_url: String,
) -> Result<()> {
    let settings = monthly::MonthlySettings {
        dz_url,
        solana_url,
        // Every SDK derives contributor rewards records under this prefix.
        contributor_rewards_prefix: b"dz_contributor_rewards".to_vec(),
    };
    let wallet = Wallet {
        connection: SolanaConnection::new(settings.solana_url.clone()),
        signer: load_keypair(&keypair)?,
        compute_unit_price_ix: None,
        verbose: false,
        fee_payer: None,
        dry_run,
    };
    let dz_connection = DoubleZeroLedgerConnection::new(settings.dz_url.clone());
    let context = MonthlyContext::try_new(&settings, &wallet, &dz_connection).await?;
    let label = format!("{year}-{month:02}");

    let distribution = context.try_fetch_monthly_distribution(year, month).await?;
    match monthly_distribute::month_action(&distribution) {
        MonthAction::Idle(reason) => {
            info!("Nothing to do for {label}: {reason}");
            return Ok(());
        }
        MonthAction::Finalize => {
            ensure!(
                context.try_finalize_month(year, month).await?,
                "{label} has not closed, since no DZ epoch has ended after it"
            );
            if dry_run {
                info!("Dry run: {label} is distributed once it is finalized");
                return Ok(());
            }
        }
        MonthAction::Distribute => {}
    }

    context
        .try_distribute_month(year, month, &Mutex::default())
        .await?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use clap::{Parser, error::ErrorKind};

    use super::*;

    #[derive(Parser)]
    struct Cli {
        #[command(subcommand)]
        command: RewardsCommands,
    }

    #[test]
    fn test_distribute_monthly_rewards_takes_both_rpcs_as_flags() {
        let cli = Cli::try_parse_from([
            "contributor-rewards",
            "distribute-monthly-rewards",
            "--year",
            "2026",
            "--month",
            "8",
            "--solana-url",
            "https://solana.example",
            "--dz-url",
            "https://ledger.example",
            "-k",
            "accountant.json",
        ])
        .unwrap();
        let RewardsCommands::DistributeMonthlyRewards {
            solana_url, dz_url, ..
        } = cli.command
        else {
            panic!("parsed another command");
        };
        assert_eq!(solana_url, "https://solana.example");
        assert_eq!(dz_url, "https://ledger.example");

        // Neither RPC falls back to the settings.
        let missing_rpcs = Cli::try_parse_from([
            "contributor-rewards",
            "distribute-monthly-rewards",
            "--year",
            "2026",
            "--month",
            "8",
        ]);
        assert_eq!(
            missing_rpcs.err().unwrap().kind(),
            ErrorKind::MissingRequiredArgument
        );
    }
}
