use std::collections::{HashMap, HashSet};

use anyhow::{Context, Result};
use borsh::BorshDeserialize;
use clap::Args;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    rpc::{SolanaConnection, SolanaConnectionOptions},
};
use doublezero_solana_sdk::{
    Pubkey, environment_2z_token_mint_key, environment_usdc_token_mint_key,
    revenue_distribution::{
        state::Distribution as ParentDistribution,
        types::{BurnRate, DoubleZeroEpoch, UnitShare16},
    },
    shred_subscription::{
        self,
        instruction::ShredSubscriptionInstructionData,
        state::{
            ShredDistribution, ShredDistributionJournal, ValidatorClientRewardsConfig,
            ValidatorPublisherRewards, find_shred_distribution_address,
            find_shred_distribution_journal_address, find_validator_publisher_rewards_address,
        },
    },
};
use futures::stream::{self, StreamExt};
use solana_client::{
    rpc_client::GetConfirmedSignaturesForAddress2Config, rpc_config::RpcTransactionConfig,
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::signature::Signature;
use solana_transaction_status_client_types::UiTransactionEncoding;
use tabled::{
    Table,
    settings::{Alignment, Style, object::Columns},
};

use super::{distribute::bitmap_bit_set, s3};

/// Default subscription-epoch lookback window; widen with `--num-epochs`.
const DEFAULT_LOOKBACK_EPOCHS: u64 = 20;

/// Max in-flight S3 fetches during the per-epoch leaf-discovery fan-out
/// (same bound and rationale as the distribute pass).
const S3_FETCH_CONCURRENCY: usize = 8;

/// Max in-flight `getTransaction` fetches when resolving claimed payouts.
const TX_FETCH_CONCURRENCY: usize = 8;

/*
   doublezero-solana shreds publisher-rewards status --node-id <PUBKEY> [--num-epochs <N>]
*/

#[derive(Debug, Args)]
pub struct StatusCommand {
    /// Validator node identity to report on.
    #[arg(long)]
    pub node_id: Pubkey,

    /// How many subscription epochs back from the current Solana epoch to
    /// scan.
    #[arg(long, default_value_t = DEFAULT_LOOKBACK_EPOCHS)]
    pub num_epochs: u64,

    #[command(flatten)]
    pub connection_options: SolanaConnectionOptions,
}

#[derive(Debug, Clone, Copy)]
enum RewardStatus {
    NotReady,
    Ready,
    Claimed,
    NoRewards,
    NoData,
}

impl RewardStatus {
    fn label(self) -> &'static str {
        match self {
            RewardStatus::NotReady => "not ready",
            RewardStatus::Ready => "ready",
            RewardStatus::Claimed => "claimed",
            RewardStatus::NoRewards => "no rewards",
            RewardStatus::NoData => "no data",
        }
    }
}

/// What we can say about the reward amount for an epoch.
enum AmountInfo {
    /// `ready`: not yet paid, so projected from on-chain pool/slots/burn math.
    Estimated {
        raw: u64,
        mint: Pubkey,
    },
    /// `claimed`: the exact paid amount is read from the distribute tx later.
    FromTx,
    Unknown,
}

#[derive(Debug, tabled::Tabled)]
struct StatusRow {
    #[tabled(rename = "EPOCH")]
    epoch: u64,
    #[tabled(rename = "LEADER SLOTS")]
    leader_slots: String,
    #[tabled(rename = "MINT")]
    mint: String,
    #[tabled(rename = "AMOUNT")]
    amount: String,
    #[tabled(rename = "STATUS")]
    status: &'static str,
}

fn mint_symbol(mint: &Pubkey, mints: &[Pubkey; 3]) -> String {
    if mint == &mints[0] {
        "2Z".to_string()
    } else if mint == &mints[1] {
        "USDC".to_string()
    } else if mint == &mints[2] {
        "WSOL".to_string()
    } else {
        format!("{}…", &mint.to_string()[..4])
    }
}

/// This validator's leaves in one epoch's sorted leaf set, with the epoch's
/// accumulation state and the per-client proportion config (for the `ready`
/// payout estimate).
struct EpochLeaves {
    epoch: u64,
    associated_dz_epoch: u64,
    is_accumulated: bool,
    client_rewards_config: ValidatorClientRewardsConfig,
    /// `(leaf_index, leader_slots, client_id)` per `(node_id, client_id)` match.
    leaves: Vec<(usize, u32, u16)>,
}

/// Per-epoch result of the S3 leaf scan. Every epoch with an on-chain
/// `ShredDistribution` produces one of these — never silently dropped.
enum EpochOutcome {
    Leader(EpochLeaves),
    NoRewards { epoch: u64 },
    NoData { epoch: u64 },
}

impl StatusCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let solana_connection = SolanaConnection::from(self.connection_options.clone());
        let dz_connection = self.connection_options.into_shred_subscription_connection();
        let commitment = dz_connection.0.commitment();

        let vpr_pda = find_validator_publisher_rewards_address(&self.node_id).0;
        let vpr = dz_connection
            .try_fetch_zero_copy_data_with_commitment::<ValidatorPublisherRewards>(
                &vpr_pda, commitment,
            )
            .await
            .with_context(|| {
                format!(
                    "failed to read validator publisher rewards for node {} (PDA {vpr_pda}); \
                     run `doublezero-solana shreds publisher-rewards configure` first",
                    self.node_id
                )
            })?;

        let network_env = dz_connection
            .try_network_environment()
            .await
            .context("detecting network environment")?;

        let mints = [
            environment_2z_token_mint_key(network_env),
            environment_usdc_token_mint_key(network_env),
            spl_token_interface::native_mint::ID,
        ];

        println!("Node ID:       {}", vpr.node_id);
        println!("Rewards owner: {}", vpr.rewards_token_owner_key);
        println!(
            "Rewards mint:  {}",
            mint_symbol(&vpr.rewards_token_mint_key, &mints)
        );

        // Subscription epoch == Solana epoch, so resolve the window against a
        // Solana RPC (the DZ-Ledger RPC hosting the program has its own
        // unrelated epoch on testnet/localnet). Same reasoning as distribute.
        let current_epoch = solana_connection
            .0
            .get_epoch_info()
            .await
            .context("fetching current Solana epoch")?
            .epoch;
        let from_epoch = current_epoch.saturating_sub(self.num_epochs);

        println!("\nScanning epochs {from_epoch}..={current_epoch}.");

        let rows = scan_epoch_status(
            &dz_connection,
            &self.node_id,
            &vpr_pda,
            &vpr.rewards_token_owner_key,
            from_epoch,
            current_epoch,
            &mints,
        )
        .await?;

        if rows.is_empty() {
            println!(
                "\nNo shred distributions found in epochs {from_epoch}..={current_epoch}. \
                 Widen the window with --num-epochs if you expected older rewards."
            );
            return Ok(());
        }

        let mut table = Table::new(&rows);
        table.with(Style::markdown());
        table.modify(Columns::one(1), Alignment::right());
        table.modify(Columns::one(3), Alignment::right());
        println!("\n{table}");

        println!(
            "\nStatus:\n  \
             ready       reward is accumulated and waiting to be distributed to your ATA\n  \
             not ready   reward for this epoch isn't distributable yet (still being collected, \
             calculated, or swapped)\n  \
             claimed     reward has already been distributed to your ATA\n  \
             no rewards  you published no shreds this epoch, so there's nothing to distribute\n  \
             no data     this epoch's leader-slot export isn't available yet"
        );

        Ok(())
    }
}

#[allow(clippy::too_many_arguments)]
async fn scan_epoch_status(
    dz_connection: &SolanaConnection,
    node_id: &Pubkey,
    vpr_pda: &Pubkey,
    rewards_token_owner_key: &Pubkey,
    from_epoch: u64,
    current_epoch: u64,
    mints: &[Pubkey; 3],
) -> Result<Vec<StatusRow>> {
    let shred_distribution_pdas = (from_epoch..=current_epoch)
        .map(|epoch| find_shred_distribution_address(epoch).0)
        .collect::<Vec<_>>();
    let shred_distribution_accounts = dz_connection
        .try_fetch_multiple_accounts(&shred_distribution_pdas)
        .await
        .context("fetching ShredDistribution accounts")?;

    let existing = shred_distribution_accounts
        .into_iter()
        .enumerate()
        .filter_map(|(offset, account)| {
            if account.data.is_empty() {
                return None;
            }
            let epoch = from_epoch + offset as u64;
            let shred_distribution: ZeroCopyAccountOwnedData<ShredDistribution> =
                account.try_into().ok()?;
            Some((epoch, shred_distribution))
        })
        .collect::<Vec<_>>();

    if existing.is_empty() {
        return Ok(Vec::new());
    }

    // Fan out S3 to find this validator's leaves per epoch.
    let s3_client = s3::build_s3_client()?;
    let mut fetch_results = stream::iter(existing.iter())
        .map(|(epoch, shred_distribution)| {
            let s3_client = s3_client.clone();
            async move {
                let result = s3::fetch_leader_slot_data(&s3_client, *epoch).await;
                (*epoch, shred_distribution, result)
            }
        })
        .buffer_unordered(S3_FETCH_CONCURRENCY)
        .collect::<Vec<_>>()
        .await;
    fetch_results.sort_by_key(|(epoch, _, _)| *epoch);

    let mut outcomes: Vec<EpochOutcome> = Vec::new();
    for (epoch, shred_distribution, fetch_result) in fetch_results {
        // A missing export is expected for the most recent epoch (it hasn't run
        // yet); surface it as `no data` rather than dropping the epoch.
        let entries = match fetch_result {
            Ok(entries) => entries,
            Err(_) => {
                outcomes.push(EpochOutcome::NoData { epoch });
                continue;
            }
        };
        let computed = match s3::compute_leaves(&entries) {
            Ok(computed) => computed,
            Err(err) => {
                eprintln!("  epoch {epoch}: leader-slot data unusable: {err:#}");
                outcomes.push(EpochOutcome::NoData { epoch });
                continue;
            }
        };
        let leaves = computed
            .leaves
            .iter()
            .enumerate()
            .filter(|(_, leaf)| &leaf.node_id == node_id)
            .map(|(leaf_index, leaf)| (leaf_index, leaf.leader_slots, leaf.client_id))
            .collect::<Vec<_>>();
        if leaves.is_empty() {
            outcomes.push(EpochOutcome::NoRewards { epoch });
        } else {
            outcomes.push(EpochOutcome::Leader(EpochLeaves {
                epoch,
                associated_dz_epoch: shred_distribution.associated_dz_epoch.value(),
                is_accumulated: shred_distribution.is_validator_rewards_accumulated(),
                client_rewards_config: shred_distribution.validator_client_rewards_config,
                leaves,
            }));
        }
    }

    // Only accumulated leader epochs need journal / parent reads. They're
    // visited in `outcomes` order both here and in the classify loop, so one
    // cursor walks their journal triples in lockstep.
    let accumulated_leader_epochs = outcomes
        .iter()
        .filter_map(|outcome| match outcome {
            EpochOutcome::Leader(leaves) if leaves.is_accumulated => Some(leaves),
            _ => None,
        })
        .collect::<Vec<_>>();

    let mut journal_pdas = Vec::with_capacity(accumulated_leader_epochs.len() * 3);
    for leaves in &accumulated_leader_epochs {
        for mint in mints {
            journal_pdas.push(find_shred_distribution_journal_address(leaves.epoch, mint).0);
        }
    }
    let journal_accounts = dz_connection
        .try_fetch_multiple_accounts(&journal_pdas)
        .await
        .context("fetching journal accounts")?;

    let mut parent_index_for_dz_epoch = HashMap::new();
    let mut parent_pdas = Vec::new();
    for leaves in &accumulated_leader_epochs {
        parent_index_for_dz_epoch
            .entry(leaves.associated_dz_epoch)
            .or_insert_with(|| {
                parent_pdas.push(
                    ParentDistribution::find_address(DoubleZeroEpoch::new(
                        leaves.associated_dz_epoch,
                    ))
                    .0,
                );
                parent_pdas.len() - 1
            });
    }
    let parent_accounts = dz_connection
        .try_fetch_multiple_accounts(&parent_pdas)
        .await
        .context("fetching parent Distribution accounts")?;

    let mut classified: Vec<(u64, String, RewardStatus, Option<Pubkey>, AmountInfo)> =
        Vec::with_capacity(outcomes.len());
    let mut accumulated_cursor = 0usize;

    for outcome in &outcomes {
        let row = match outcome {
            EpochOutcome::NoData { epoch } => (
                *epoch,
                "—".to_string(),
                RewardStatus::NoData,
                None,
                AmountInfo::Unknown,
            ),
            EpochOutcome::NoRewards { epoch } => (
                *epoch,
                "0".to_string(),
                RewardStatus::NoRewards,
                None,
                AmountInfo::Unknown,
            ),
            EpochOutcome::Leader(leaves) => {
                let leader_slots: u32 = leaves.leaves.iter().map(|(_, slots, _)| slots).sum();
                let (status, mint, amount) = if !leaves.is_accumulated {
                    (RewardStatus::NotReady, None, AmountInfo::Unknown)
                } else {
                    let journal_base = accumulated_cursor * 3;
                    accumulated_cursor += 1;
                    classify_accumulated(
                        leaves,
                        &journal_accounts[journal_base..journal_base + 3],
                        &parent_accounts,
                        &parent_index_for_dz_epoch,
                    )
                };
                (leaves.epoch, leader_slots.to_string(), status, mint, amount)
            }
        };
        classified.push(row);
    }

    // `claimed` rewards already moved, so the exact figure lives in the
    // distribute tx — no math, no drift.
    let claimed_epochs = classified
        .iter()
        .filter_map(|(epoch, _, _, _, amount)| {
            matches!(amount, AmountInfo::FromTx).then_some(*epoch)
        })
        .collect::<HashSet<_>>();

    let resolved_payouts = if claimed_epochs.is_empty() {
        HashMap::new()
    } else {
        let pda_to_epoch = claimed_epochs
            .iter()
            .map(|&epoch| (find_shred_distribution_address(epoch).0, epoch))
            .collect::<HashMap<_, _>>();
        let signature_limit = ((current_epoch - from_epoch + 1) as usize * 4).clamp(50, 1000);
        resolve_distributed_payouts(
            dz_connection,
            vpr_pda,
            &pda_to_epoch,
            rewards_token_owner_key,
            signature_limit,
        )
        .await
    };

    let needs_amounts = classified
        .iter()
        .any(|(_, _, _, _, amount)| !matches!(amount, AmountInfo::Unknown));
    let decimals_by_mint = if needs_amounts {
        fetch_mint_decimals(dz_connection, mints).await
    } else {
        HashMap::new()
    };

    // For claimed epochs the tx scan is authoritative for both mint and amount;
    // everything else falls back to the journal verdict.
    let rows = classified
        .into_iter()
        .map(|(epoch, leader_slots, status, mint, amount)| {
            let resolved = resolved_payouts.get(&epoch);
            let mint = match (resolved, mint) {
                (Some((mint, _)), _) => mint_symbol(mint, mints),
                (None, Some(mint)) => mint_symbol(&mint, mints),
                (None, None) => "—".to_string(),
            };
            let amount = match (resolved, amount) {
                (Some((mint, raw)), _) => format_amount(*raw, &decimals_by_mint, mint),
                (None, AmountInfo::Estimated { raw, mint }) => {
                    format!("~{}", format_amount(raw, &decimals_by_mint, &mint))
                }
                (None, _) => "—".to_string(),
            };
            StatusRow {
                epoch,
                leader_slots,
                mint,
                amount,
                status: status.label(),
            }
        })
        .collect();

    Ok(rows)
}

/// Reads the SPL mint's `decimals` byte (offset 44 in the canonical layout).
/// Unresolved mints are absent — formatting then falls back to the raw amount.
async fn fetch_mint_decimals(
    dz_connection: &SolanaConnection,
    mints: &[Pubkey; 3],
) -> HashMap<Pubkey, u8> {
    const MINT_DECIMALS_OFFSET: usize = 44;
    let accounts = match dz_connection.try_fetch_multiple_accounts(mints).await {
        Ok(accounts) => accounts,
        Err(_) => return HashMap::new(),
    };
    mints
        .iter()
        .zip(accounts)
        .filter_map(|(mint, account)| {
            account
                .data
                .get(MINT_DECIMALS_OFFSET)
                .map(|decimals| (*mint, *decimals))
        })
        .collect()
}

/// Format a raw token amount to at most three (truncated) fractional digits with
/// trailing zeros trimmed, falling back to the raw integer when decimals are
/// unknown. Keeping three digits stops small-but-real payouts (e.g. 0.05 SOL at
/// 9 decimals) from collapsing to `0`.
fn format_amount(raw: u64, decimals_by_mint: &HashMap<Pubkey, u8>, mint: &Pubkey) -> String {
    let Some(&decimals) = decimals_by_mint.get(mint) else {
        return raw.to_string();
    };
    if decimals == 0 {
        return raw.to_string();
    }
    let scale = 10u128.pow(decimals as u32);
    let raw = raw as u128;
    let integer = raw / scale;
    // Truncate the fraction to three digits, then drop trailing zeros.
    let frac = (raw % scale) * 1_000 / scale;
    if frac == 0 {
        return integer.to_string();
    }
    let frac = format!("{frac:03}");
    format!("{integer}.{}", frac.trim_end_matches('0'))
}

/// Resolve `(epoch → (reward mint, exact paid amount))` for `claimed` epochs by
/// replaying the validator's distribute history. Anchored on the
/// `ValidatorPublisherRewards` PDA, which every `DistributeValidatorRewards`
/// references, so the signature set is validator-specific. The epoch comes from
/// the `ShredDistribution` PDA in the ix accounts; the mint and amount from the
/// destination-ATA token-balance delta. Best-effort: transactions beyond the
/// RPC's history retention stay unresolved (rendered as `—`).
async fn resolve_distributed_payouts(
    dz_connection: &SolanaConnection,
    vpr_pda: &Pubkey,
    pda_to_epoch: &HashMap<Pubkey, u64>,
    rewards_token_owner_key: &Pubkey,
    signature_limit: usize,
) -> HashMap<u64, (Pubkey, u64)> {
    let sigs_config = GetConfirmedSignaturesForAddress2Config {
        limit: Some(signature_limit),
        commitment: Some(CommitmentConfig::confirmed()),
        ..Default::default()
    };
    let signatures = match dz_connection
        .0
        .get_signatures_for_address_with_config(vpr_pda, sigs_config)
        .await
    {
        Ok(signatures) => signatures,
        Err(err) => {
            eprintln!(
                "  warning: couldn't read distribute history ({err:#}); some amounts unknown"
            );
            return HashMap::new();
        }
    };

    let tx_config = RpcTransactionConfig {
        encoding: Some(UiTransactionEncoding::Base64),
        commitment: Some(CommitmentConfig::confirmed()),
        max_supported_transaction_version: Some(0),
    };

    let transactions = stream::iter(signatures.into_iter().filter(|sig| sig.err.is_none()))
        .map(|sig_info| async move {
            let signature: Signature = sig_info.signature.parse().ok()?;
            dz_connection
                .0
                .get_transaction_with_config(&signature, tx_config)
                .await
                .ok()
        })
        .buffer_unordered(TX_FETCH_CONCURRENCY)
        .collect::<Vec<_>>()
        .await;

    let owner = rewards_token_owner_key.to_string();
    let mut resolved: HashMap<u64, (Pubkey, u64)> = HashMap::new();
    for response in transactions.into_iter().flatten() {
        let meta = response.transaction.meta;
        let Some(versioned_tx) = response.transaction.transaction.decode() else {
            continue;
        };
        let message = versioned_tx.message;
        let account_keys = message.static_account_keys();

        let mut epoch = None;
        for ix in message.instructions() {
            let program_id = account_keys
                .get(ix.program_id_index as usize)
                .copied()
                .unwrap_or_default();
            if program_id != *shred_subscription::ID {
                continue;
            }
            if !matches!(
                ShredSubscriptionInstructionData::try_from_slice(&ix.data),
                Ok(ShredSubscriptionInstructionData::DistributeValidatorRewards { .. })
            ) {
                continue;
            }
            for &account_index in &ix.accounts {
                if let Some(epoch_for_key) = account_keys
                    .get(account_index as usize)
                    .and_then(|key| pda_to_epoch.get(key))
                {
                    epoch = Some(*epoch_for_key);
                }
            }
        }
        let Some(epoch) = epoch else { continue };

        // The publisher payout is the credit to the destination ATA — the only
        // token account in the tx owned by `rewards_token_owner_key`.
        let Some(meta) = meta else { continue };
        let post = Option::<Vec<_>>::from(meta.post_token_balances).unwrap_or_default();
        let pre = Option::<Vec<_>>::from(meta.pre_token_balances).unwrap_or_default();
        let Some(destination) = post.iter().find(|balance| {
            Option::<String>::from(balance.owner.clone()).as_deref() == Some(owner.as_str())
        }) else {
            continue;
        };
        let post_raw = destination
            .ui_token_amount
            .amount
            .parse::<u128>()
            .unwrap_or_default();
        let pre_raw = pre
            .iter()
            .find(|balance| balance.account_index == destination.account_index)
            .and_then(|balance| balance.ui_token_amount.amount.parse::<u128>().ok())
            .unwrap_or_default();
        let paid = post_raw.saturating_sub(pre_raw) as u64;
        if let Ok(mint) = destination.mint.parse::<Pubkey>() {
            // A multi-client validator has multiple leaves per epoch, and
            // distribute emits one tx per leaf crediting the same ATA, so sum
            // them to match `estimate_publisher_payout`'s per-leaf total.
            resolved
                .entry(epoch)
                .and_modify(|(_, amount)| *amount += paid)
                .or_insert((mint, paid));
        }
    }
    resolved
}

/// The per-`client_id` publisher/client split, mirroring the program's
/// `proportion_at_or_default`: an override applies only when an entry's `id`
/// matches and its `set_bitmap` slot is set; otherwise `default_proportion`
/// (with the program's legacy 35% fallback when the default is zero).
fn client_proportion(config: &ValidatorClientRewardsConfig, client_id: u16) -> u64 {
    const LEGACY_DEFAULT_PROPORTION: u64 = 3_500;
    let override_proportion = config
        .proportions
        .proportions
        .iter()
        .enumerate()
        .find(|(slot, entry)| {
            entry.id == client_id && config.proportions.set_bitmap & (1u32 << slot) != 0
        })
        .map(|(_, entry)| u64::from(entry.rewards_proportion));
    override_proportion.unwrap_or_else(|| {
        let default = u64::from(config.default_proportion);
        if default == 0 {
            LEGACY_DEFAULT_PROPORTION
        } else {
            default
        }
    })
}

/// Project the post-burn publisher payout for a `ready` epoch from the journal
/// it routes to, mirroring the program's per-leaf `try_validator_share_pre_burn`
/// then burn, summed over the validator's leaves. An estimate because the burn
/// rate is read live at distribute time.
fn estimate_publisher_payout(
    journal: &ShredDistributionJournal,
    config: &ValidatorClientRewardsConfig,
    leaves: &[(usize, u32, u16)],
    burn_rate: BurnRate,
) -> u64 {
    let rewards_amount = if journal.is_swap_bypassed() {
        journal.checked_usdc_swap_budget().unwrap_or_default()
    } else {
        journal.tokens_received_amount
    };
    let denominator = u128::from(journal.accumulated_publisher_slots_scaled)
        + u128::from(journal.accumulated_client_slots_scaled);
    if denominator == 0 {
        return 0;
    }
    let max = u64::from(UnitShare16::MAX);

    leaves
        .iter()
        .map(|(_, leader_slots, client_id)| {
            let publisher_scaled =
                u64::from(*leader_slots) * (max - client_proportion(config, *client_id));
            let pre_burn =
                (u128::from(publisher_scaled) * u128::from(rewards_amount) / denominator) as u64;
            pre_burn - burn_rate.mul_scalar(pre_burn)
        })
        .sum()
}

/// Classify an accumulated epoch into its status, reward mint, and amount.
///
/// A clear bitmap bit in one journal is ambiguous (never routed there vs.
/// already distributed), so the verdict is collapsed across all three: an
/// accumulated leaf with no bit set anywhere can only have been distributed.
/// The mint is the journal's `reward_mint_key`; for `claimed` it's only known
/// when every swap-complete journal agrees (else resolved from tx history).
fn classify_accumulated(
    epoch: &EpochLeaves,
    journals: &[solana_sdk::account::Account],
    parent_accounts: &[solana_sdk::account::Account],
    parent_index_for_dz_epoch: &HashMap<u64, usize>,
) -> (RewardStatus, Option<Pubkey>, AmountInfo) {
    // Distribute won't pay until the parent distribution is finalized.
    let parent = parent_index_for_dz_epoch
        .get(&epoch.associated_dz_epoch)
        .and_then(|&index| parent_accounts.get(index))
        .filter(|account| !account.data.is_empty())
        .and_then(ZeroCopyAccountOwnedData::<ParentDistribution>::from_account)
        .filter(|parent| parent.is_rewards_calculation_finalized());
    let Some(parent) = parent else {
        return (RewardStatus::NotReady, None, AmountInfo::Unknown);
    };
    let burn_rate = parent.burn_rate(BurnRate::default());

    let mut ready: Option<(Pubkey, u64)> = None;
    let mut swap_complete_reward_mints: Vec<Pubkey> = Vec::new();

    for journal_account in journals {
        if journal_account.data.is_empty() {
            continue;
        }
        let Some(journal) =
            ZeroCopyAccountOwnedData::<ShredDistributionJournal>::from_account(journal_account)
        else {
            continue;
        };
        if !journal.is_swap_complete() {
            continue;
        }
        swap_complete_reward_mints.push(journal.reward_mint_key);

        let Some(bitmap_range) = journal.checked_publisher_accumulation_bitmap_range() else {
            continue;
        };
        let Some(bitmap) = journal
            .remaining_data
            .get(bitmap_range.start..bitmap_range.end)
        else {
            continue;
        };
        if ready.is_none()
            && epoch
                .leaves
                .iter()
                .any(|(leaf_index, _, _)| bitmap_bit_set(bitmap, *leaf_index))
        {
            let payout = estimate_publisher_payout(
                &journal,
                &epoch.client_rewards_config,
                &epoch.leaves,
                burn_rate,
            );
            ready = Some((journal.reward_mint_key, payout));
        }
    }

    if let Some((mint, payout)) = ready {
        (
            RewardStatus::Ready,
            Some(mint),
            AmountInfo::Estimated { raw: payout, mint },
        )
    } else if let Some(&first) = swap_complete_reward_mints.first() {
        let mint = swap_complete_reward_mints
            .iter()
            .all(|m| *m == first)
            .then_some(first);
        (RewardStatus::Claimed, mint, AmountInfo::FromTx)
    } else {
        (RewardStatus::NotReady, None, AmountInfo::Unknown)
    }
}

#[cfg(test)]
mod tests {
    use doublezero_solana_sdk::shred_subscription::state::ValidatorClientRewardsProportion;

    use super::*;

    fn unit_share(value: u16) -> UnitShare16 {
        UnitShare16::new(value).expect("value within UnitShare16 range")
    }

    #[test]
    fn client_proportion_uses_default_when_no_override() {
        let mut config = ValidatorClientRewardsConfig::default();
        config.default_proportion = unit_share(2_000);
        assert_eq!(client_proportion(&config, 7), 2_000);
    }

    #[test]
    fn client_proportion_legacy_fallback_when_default_zero() {
        let config = ValidatorClientRewardsConfig::default();
        assert_eq!(client_proportion(&config, 7), 3_500);
    }

    #[test]
    fn client_proportion_override_requires_matching_id_and_set_bit() {
        let mut config = ValidatorClientRewardsConfig::default();
        config.default_proportion = unit_share(2_000);
        config.proportions.proportions[0] = ValidatorClientRewardsProportion {
            id: 7,
            rewards_proportion: unit_share(1_000),
        };

        // Entry present but its set_bitmap slot is clear -> ignored.
        assert_eq!(client_proportion(&config, 7), 2_000);

        // Slot marked set -> override applies, but only for the matching id.
        config.proportions.set_bitmap |= 1 << 0;
        assert_eq!(client_proportion(&config, 7), 1_000);
        assert_eq!(client_proportion(&config, 8), 2_000);
    }

    #[test]
    fn format_amount_trims_to_three_fractional_digits() {
        let mint = Pubkey::new_unique();
        let decimals = HashMap::from([(mint, 6u8)]);
        assert_eq!(format_amount(1_234_560, &decimals, &mint), "1.234"); // truncates, not rounds
        assert_eq!(format_amount(1_000_000, &decimals, &mint), "1"); // trailing zeros trimmed
        assert_eq!(format_amount(500_000, &decimals, &mint), "0.5");
        assert_eq!(format_amount(0, &decimals, &mint), "0");
    }

    #[test]
    fn format_amount_keeps_small_nonzero_amounts_visible() {
        let mint = Pubkey::new_unique();
        // 9 decimals (e.g. WSOL): a sub-0.1 amount must not collapse to "0".
        let decimals = HashMap::from([(mint, 9u8)]);
        assert_eq!(format_amount(50_000_000, &decimals, &mint), "0.05");
        assert_eq!(format_amount(1_000_000, &decimals, &mint), "0.001");
    }

    #[test]
    fn format_amount_falls_back_to_raw_when_decimals_unknown() {
        let mint = Pubkey::new_unique();
        assert_eq!(format_amount(42, &HashMap::new(), &mint), "42");
    }
}
