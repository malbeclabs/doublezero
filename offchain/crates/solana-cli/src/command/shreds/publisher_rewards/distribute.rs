// Not a standalone subcommand: `try_distribute_pending` is invoked only by
// `configure` as a post-configure pass. Subscription epoch == Solana epoch
// here, so the current epoch is resolved via `getEpochInfo` on a Solana RPC,
// not the DZ-Ledger RPC that hosts the program (on testnet/localnet those are
// distinct chains with independent epoch numbers).

use std::collections::{HashMap, HashSet};

use anyhow::{Context, Result};
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    payer::{TransactionOutcome, Wallet},
    rpc::{NetworkEnvironment, SolanaConnection},
};
use doublezero_solana_sdk::{
    Pubkey, environment_2z_token_mint_key, environment_usdc_token_mint_key,
    merkle::MerkleProof,
    revenue_distribution::{state::Distribution as ParentDistribution, types::DoubleZeroEpoch},
    shred_subscription::{
        ID,
        instruction::{
            ShredSubscriptionInstructionData,
            account::{
                DistributeValidatorRewardsAccountsInitializer, InitializeClaimHoldingAccounts,
            },
        },
        state::{
            ShredDistribution, ShredDistributionJournal, find_claim_holding_address,
            find_program_config_address, find_shred_distribution_address,
            find_shred_distribution_journal_address, find_validator_client_rewards_address,
            is_distribute_validator_rewards_enabled,
        },
        types::ValidatorRewardsLeaf,
    },
    try_build_instruction,
};
use futures::stream::{self, StreamExt};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::instruction::Instruction;
use spl_associated_token_account_interface::{
    address::get_associated_token_address, instruction::create_associated_token_account_idempotent,
};

use super::s3;

/// Subscription-epoch lookback window for the post-configure distribute
/// pass. Sized to fit one `getMultipleAccounts` chunk (100 keys) for the
/// `ShredDistribution` batch; journal batches at 3 mints fit in 3 chunks.
const DISTRIBUTE_LOOKBACK_EPOCHS: u64 = 100;

/// Max in-flight S3 fetches during the leaf-discovery fan-out. The full
/// lookback at typical S3 latencies (~50-200 ms per request) is
/// 5-20 seconds wall-clock when sequential; 8 concurrent fetches cuts
/// that roughly 8x while staying polite to S3.
const S3_FETCH_CONCURRENCY: usize = 8;

/// Counters surfaced at the end of a distribute pass.
/// - `distributed`: distribute txs that landed.
/// - `failed`: distribute txs that errored, plus epochs we couldn't even
///   evaluate (S3 fetch / merkle-leaf failures). Each has a logged reason.
///
/// Epochs with nothing to do — the leaf's bitmap bit is already clear
/// because it was distributed in a prior run or never routed to this
/// journal — are intentionally NOT counted. A clear bit means "settled",
/// so counting it would make every re-run report a growing pile of
/// phantom "unsettled" epochs.
#[derive(Debug, Default)]
pub struct DistributeOutcome {
    pub distributed: u32,
    pub failed: u32,
}

/// Per-(epoch, leaf) bundle of everything needed to build a distribute tx
/// after the upfront batched fetches complete.
struct Candidate {
    subscription_epoch: u64,
    associated_dz_epoch: u64,
    leaf_index: usize,
    leaf: ValidatorRewardsLeaf,
    proof: MerkleProof,
}

pub async fn try_distribute_pending(
    wallet: &Wallet,
    solana_connection: &SolanaConnection,
    node_id: &Pubkey,
    rewards_token_owner_key: &Pubkey,
    network_env: NetworkEnvironment,
) -> Result<DistributeOutcome> {
    let mut outcome = DistributeOutcome::default();

    // Check if distribute flag is enabled in ProgramConfig.
    let program_config_account = wallet
        .connection
        .try_fetch_multiple_accounts(&[find_program_config_address().0])
        .await
        .context("fetching program config")?;
    let distribute_enabled = program_config_account
        .first()
        .is_some_and(|account| is_distribute_validator_rewards_enabled(&account.data));
    if !distribute_enabled {
        println!(
            "\nDistribute is not enabled on this cluster yet; skipping pending-rewards distribution."
        );
        return Ok(outcome);
    }

    // `wallet.connection` is the DZ-Ledger RPC (program host). Its epoch
    // is the DZ-Ledger epoch, which has no relation to the Solana epoch
    // on testnet/localnet. `subscription_epoch` PDAs and S3 file names
    // are Solana-epoch keyed, so we must ask a Solana RPC.
    let current_epoch = solana_connection
        .0
        .get_epoch_info()
        .await
        .context("fetching current Solana epoch")?
        .epoch;
    let from_epoch = current_epoch.saturating_sub(DISTRIBUTE_LOOKBACK_EPOCHS);

    println!("\nScanning epochs {from_epoch}..={current_epoch} for unsettled validator rewards.");

    // Mints we probe each epoch. We don't assume the validator's current
    // VPR mint matches what they were configured against historically;
    // accumulate may have routed earlier-epoch rewards into a different
    // journal. Probing all three covers that case.
    let dz_mint_key = environment_2z_token_mint_key(network_env);
    let usdc_mint_key = environment_usdc_token_mint_key(network_env);
    let wsol_mint_key = spl_token_interface::native_mint::ID;
    let journal_mint_candidates = [dz_mint_key, usdc_mint_key, wsol_mint_key];

    // ----- Step 1: batch fetch ShredDistribution accounts in the window -----

    let shred_distribution_pdas: Vec<Pubkey> = (from_epoch..=current_epoch)
        .map(|epoch| find_shred_distribution_address(epoch).0)
        .collect();
    let shred_distribution_accounts = wallet
        .connection
        .try_fetch_multiple_accounts(&shred_distribution_pdas)
        .await
        .context("fetching candidate ShredDistribution accounts")?;

    let accumulated: Vec<(u64, ZeroCopyAccountOwnedData<ShredDistribution>)> =
        shred_distribution_accounts
            .into_iter()
            .enumerate()
            .filter_map(|(offset, account)| {
                if account.data.is_empty() {
                    return None;
                }
                let epoch = from_epoch + offset as u64;
                let shred_distribution: ZeroCopyAccountOwnedData<ShredDistribution> =
                    account.try_into().ok()?;
                shred_distribution
                    .is_validator_rewards_accumulated()
                    .then_some((epoch, shred_distribution))
            })
            .collect();

    if accumulated.is_empty() {
        println!("No accumulated epochs in the window; nothing to distribute.");
        return Ok(outcome);
    }

    // ----- Step 2: fan out S3 to find this validator's leaf per epoch -----

    let s3_client = s3::build_s3_client()?;

    // Issue all S3 fetches concurrently (capped at `S3_FETCH_CONCURRENCY`),
    // carrying the `shred_distribution` reference alongside the fetch
    // result so the post-fetch loop doesn't have to re-scan
    // `accumulated`. `reqwest::Client` clones are Arc-cheap. Each tuple
    // element: `(epoch, &shred_distribution, fetch_result)`.
    let mut fetch_results = stream::iter(accumulated.iter())
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
    // `buffer_unordered` yields in completion order; sort ascending by
    // epoch so the per-epoch logs below print chronologically.
    fetch_results.sort_by_key(|(epoch, _, _)| *epoch);

    let mut candidates: Vec<Candidate> = Vec::new();
    for (epoch, shred_distribution, fetch_result) in fetch_results {
        let entries = match fetch_result {
            Ok(entries) => entries,
            Err(err) => {
                eprintln!("  epoch {epoch}: failed to fetch S3 leaves: {err:#}");
                outcome.failed += 1;
                continue;
            }
        };
        // Build the sorted leaf set once per epoch, then compute proofs
        // ONLY for the leaves matching this validator. With ~1500
        // validators per epoch × 100 epochs of lookback, computing all
        // proofs up front would be tens of millions of SHA-256 ops per
        // configure call; this restricts us to O(log N) work per matched
        // leaf (typically 1, occasionally 2+ for multi-client-id
        // validators).
        let computed = match s3::compute_leaves(&entries) {
            Ok(c) => c,
            Err(err) => {
                eprintln!("  epoch {epoch}: failed to compute merkle leaves: {err:#}");
                outcome.failed += 1;
                continue;
            }
        };
        // A validator can legitimately appear under multiple client_ids
        // in the same epoch — the leaf schema's dedup key is
        // `(node_id, client_id)`, not `node_id` alone. Each `(node_id,
        // client_id)` leaf has its own merkle proof and its own bit in
        // the journal's bitmap, so we emit one `Candidate` per match
        // here. Downstream batching and the per-leaf bitmap check
        // already handle each `Candidate` independently.
        for (leaf_index, leaf) in computed.leaves.iter().enumerate() {
            if &leaf.node_id != node_id {
                continue;
            }
            let proof = match s3::compute_proof_for_leaf(&computed.leaves, leaf_index) {
                Ok(proof) => proof,
                Err(err) => {
                    eprintln!(
                        "  epoch {epoch} leaf {leaf_index}: failed to compute proof: {err:#}"
                    );
                    continue;
                }
            };
            candidates.push(Candidate {
                subscription_epoch: epoch,
                associated_dz_epoch: shred_distribution.associated_dz_epoch.value(),
                leaf_index,
                leaf: *leaf,
                proof,
            });
        }
        // If no leaves matched, the validator wasn't a leader this
        // epoch — silent skip (no work to do, not an error).
    }

    if candidates.is_empty() {
        println!("No candidate epochs with this validator as a leaf; nothing to distribute.");
        return Ok(outcome);
    }

    // ----- Step 3: batch fetch journals at all three mints per candidate -----
    //
    // Layout: journal_accounts[candidate_idx * 3 + mint_idx]. The internal
    // chunking in `try_fetch_multiple_accounts` keeps a single call under
    // the 100-key getMultipleAccounts limit, so a 100-candidate × 3-mint
    // (= 300-key) batch lands in three RPCs.

    let mut journal_pdas: Vec<Pubkey> = Vec::with_capacity(candidates.len() * 3);
    for candidate in &candidates {
        for mint in &journal_mint_candidates {
            journal_pdas.push(
                find_shred_distribution_journal_address(candidate.subscription_epoch, mint).0,
            );
        }
    }
    let journal_accounts = wallet
        .connection
        .try_fetch_multiple_accounts(&journal_pdas)
        .await
        .context("fetching journal accounts")?;

    // ----- Step 4: batch fetch parent distributions, deduped by DZ epoch -----

    let mut parent_index_for_dz_epoch: HashMap<u64, usize> = HashMap::new();
    let mut parent_pdas: Vec<Pubkey> = Vec::new();
    for candidate in &candidates {
        parent_index_for_dz_epoch
            .entry(candidate.associated_dz_epoch)
            .or_insert_with(|| {
                let pda = ParentDistribution::find_address(DoubleZeroEpoch::new(
                    candidate.associated_dz_epoch,
                ))
                .0;
                parent_pdas.push(pda);
                parent_pdas.len() - 1
            });
    }
    let parent_accounts = wallet
        .connection
        .try_fetch_multiple_accounts(&parent_pdas)
        .await
        .context("fetching parent Distribution accounts")?;

    // ----- Step 5: batch fetch claim holdings (one per candidate) -----

    let claim_holding_pdas: Vec<Pubkey> = candidates
        .iter()
        .map(|candidate| {
            let validator_client_rewards_key =
                find_validator_client_rewards_address(candidate.leaf.client_id).0;
            find_claim_holding_address(
                &validator_client_rewards_key,
                candidate.subscription_epoch,
                &dz_mint_key,
            )
            .0
        })
        .collect();
    let claim_holding_accounts = wallet
        .connection
        .try_fetch_multiple_accounts(&claim_holding_pdas)
        .await
        .context("fetching claim holding accounts")?;

    // ----- Step 6: pre-fetch destination ATAs -----
    //
    // Across the entire pass there are at most three distinct
    // destination ATAs (one per candidate reward mint:
    // `get_associated_token_address(rewards_token_owner_key, mint)`).
    // Probe them once via a single `getMultipleAccounts` so per-tx
    // we only emit `create_associated_token_account_idempotent` when
    // the destination is actually missing — saves ~25k CU per tx that
    // would otherwise pay the SPL Token existence check on a no-op
    // create. The set is mutated as freshly-created ATAs land.
    let candidate_destination_atas: Vec<Pubkey> = journal_mint_candidates
        .iter()
        .map(|mint| get_associated_token_address(rewards_token_owner_key, mint))
        .collect();
    let candidate_destination_ata_accounts = wallet
        .connection
        .try_fetch_multiple_accounts(&candidate_destination_atas)
        .await
        .context("pre-fetching destination ATAs")?;
    let mut known_existing_atas: HashSet<Pubkey> = candidate_destination_atas
        .iter()
        .zip(candidate_destination_ata_accounts.iter())
        .filter_map(|(ata, account)| (!account.data.is_empty()).then_some(*ata))
        .collect();

    // ----- Step 7: in-memory decision loop, submit per (epoch, mint) -----
    //
    // `InitializeClaimHolding` is idempotent on-chain but a re-issued init
    // still costs a CPI and tx bytes. Dedup by `(epoch, client_id)` —
    // the claim_holding PDA is seeded by `validator_client_rewards_key`
    // (per-client_id) + epoch + 2Z mint, so a validator that appears
    // under two client_ids in the same epoch has TWO separate claim
    // holdings and needs an init for each. Keying by epoch alone would
    // wrongly skip the second init.

    let mut emitted_init_for_holding: HashSet<(u64, u16)> = HashSet::new();

    for (candidate_index, candidate) in candidates.iter().enumerate() {
        // Parent distribution gate.
        let parent_index = parent_index_for_dz_epoch[&candidate.associated_dz_epoch];
        let parent_account = &parent_accounts[parent_index];
        if parent_account.data.is_empty() {
            println!(
                "  epoch {}: skipped — parent distribution missing",
                candidate.subscription_epoch
            );
            continue;
        }
        let parent_distribution: ZeroCopyAccountOwnedData<ParentDistribution> =
            match parent_account.clone().try_into() {
                Ok(data) => data,
                Err(_) => {
                    println!(
                        "  epoch {}: skipped — parent distribution malformed",
                        candidate.subscription_epoch
                    );
                    continue;
                }
            };
        if !parent_distribution.is_rewards_calculation_finalized() {
            println!(
                "  epoch {}: skipped — parent rewards not finalized",
                candidate.subscription_epoch
            );
            continue;
        }

        let claim_holding_exists = !claim_holding_accounts[candidate_index].data.is_empty();

        for (mint_index, publisher_mint) in journal_mint_candidates.iter().enumerate() {
            let journal_account = &journal_accounts[candidate_index * 3 + mint_index];
            if journal_account.data.is_empty() {
                continue;
            }
            let publisher_journal: ZeroCopyAccountOwnedData<ShredDistributionJournal> =
                match journal_account.clone().try_into() {
                    Ok(data) => data,
                    Err(_) => continue,
                };
            if !publisher_journal.is_swap_complete() {
                continue;
            }
            let Some(bitmap_range) =
                publisher_journal.checked_publisher_accumulation_bitmap_range()
            else {
                continue;
            };
            let bitmap = match publisher_journal
                .remaining_data
                .get(bitmap_range.start..bitmap_range.end)
            {
                Some(slice) => slice,
                None => continue,
            };
            if !bitmap_bit_set(bitmap, candidate.leaf_index) {
                continue;
            }

            let init_key = (candidate.subscription_epoch, candidate.leaf.client_id);
            let needs_init = !claim_holding_exists && !emitted_init_for_holding.contains(&init_key);
            let destination_ata = get_associated_token_address(
                rewards_token_owner_key,
                &publisher_journal.reward_mint_key,
            );
            let needs_ata_create = !known_existing_atas.contains(&destination_ata);
            match submit_distribute_tx(
                wallet,
                candidate,
                &publisher_journal,
                publisher_mint,
                rewards_token_owner_key,
                node_id,
                &dz_mint_key,
                needs_init,
                needs_ata_create,
            )
            .await
            {
                Ok(()) => {
                    outcome.distributed += 1;
                    if needs_init {
                        emitted_init_for_holding.insert(init_key);
                    }
                    if needs_ata_create {
                        // Now-created ATA exists for the rest of the pass.
                        known_existing_atas.insert(destination_ata);
                    }
                }
                Err(error) => {
                    let full = format!("{error:#}");
                    let summary = full.lines().next().unwrap_or(&full);
                    eprintln!(
                        "  epoch {} mint {publisher_mint}: failed: {summary}",
                        candidate.subscription_epoch
                    );
                    outcome.failed += 1;
                }
            }
        }
    }

    Ok(outcome)
}

// Builds and submits one distribute tx. The CLI version check is NOT
// prepended here — the configure tx that ran before this pass already
// enforced version compatibility for the operator session, so re-running
// it ~300 times during the pass is wasted CU. Future callers that invoke
// `try_distribute_pending` outside the configure flow are responsible for
// running their own version gate at the top of the pass.
#[allow(clippy::too_many_arguments)]
async fn submit_distribute_tx(
    wallet: &Wallet,
    candidate: &Candidate,
    publisher_journal: &ZeroCopyAccountOwnedData<ShredDistributionJournal>,
    publisher_mint_key: &Pubkey,
    rewards_token_owner_key: &Pubkey,
    node_id: &Pubkey,
    dz_mint_key: &Pubkey,
    needs_init: bool,
    needs_ata_create: bool,
) -> Result<()> {
    let mut instructions: Vec<Instruction> = Vec::new();

    if needs_init {
        let init_ix = try_build_instruction(
            &ID,
            InitializeClaimHoldingAccounts::new(
                candidate.leaf.client_id,
                candidate.subscription_epoch,
                dz_mint_key,
                &wallet.pubkey(),
            ),
            &ShredSubscriptionInstructionData::InitializeClaimHolding(candidate.subscription_epoch),
        )?;
        instructions.push(init_ix);
    }

    // The destination ATA is at `(rewards_token_owner_key, journal's
    // reward_mint_key)` — which can differ from the validator's currently
    // configured mint when we're distributing from a journal seeded
    // historically against a different mint. `configure` only creates the
    // ATA for the current mint, so we (idempotently) create whatever
    // destination this specific tx needs. Skipped when we already know
    // the destination ATA exists (pre-fetched at the top of the pass,
    // plus any ATA freshly created earlier in the same pass).
    if needs_ata_create {
        instructions.push(create_associated_token_account_idempotent(
            &wallet.pubkey(),
            rewards_token_owner_key,
            &publisher_journal.reward_mint_key,
            &spl_token_interface::ID,
        ));
    }

    let distribute_ix = try_build_instruction(
        &ID,
        DistributeValidatorRewardsAccountsInitializer {
            subscription_epoch: candidate.subscription_epoch,
            associated_dz_epoch: candidate.associated_dz_epoch,
            node_id,
            client_id: candidate.leaf.client_id,
            rewards_token_owner_key,
            publisher_mint_key,
            publisher_reward_mint_key: &publisher_journal.reward_mint_key,
            // Builder applies the omit-rule when this equals
            // `publisher_mint_key`.
            client_mint_key: dz_mint_key,
        },
        &ShredSubscriptionInstructionData::DistributeValidatorRewards {
            leader_slots: candidate.leaf.leader_slots,
            proof: candidate.proof.clone(),
        },
    )?;
    instructions.push(distribute_ix);

    // Per-ix headroom: ~30k init_claim_holding (only when `needs_init`),
    // ~25k create_ata_idempotent (only when `needs_ata_create`), ~150k
    // distribute. Same upper bounds as the admin command's submission loop.
    let cu_limit =
        150_000 + if needs_init { 30_000 } else { 0 } + if needs_ata_create { 25_000 } else { 0 };
    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(cu_limit));
    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    // Fetch a fresh blockhash per tx. The pass submits sequentially and a
    // validator with many pending epochs can run long enough that a single
    // blockhash cached up front ages out of the cluster's validity window
    // mid-pass — every later tx then fails with "Blockhash not found".
    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;
    if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
        println!(
            "  epoch {} mint {publisher_mint_key}: distributed ({tx_sig})",
            candidate.subscription_epoch
        );
        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

pub(crate) fn bitmap_bit_set(bitmap: &[u8], leaf_index: usize) -> bool {
    let byte_idx = leaf_index / 8;
    let bit_idx = leaf_index % 8;
    bitmap
        .get(byte_idx)
        .map(|b| (b >> bit_idx) & 1 == 1)
        .unwrap_or(false)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_bitmap_bit_set_in_range() {
        let bitmap = vec![0b0000_1001, 0b0000_0010];
        assert!(bitmap_bit_set(&bitmap, 0));
        assert!(!bitmap_bit_set(&bitmap, 1));
        assert!(bitmap_bit_set(&bitmap, 3));
        assert!(!bitmap_bit_set(&bitmap, 8));
        assert!(bitmap_bit_set(&bitmap, 9));
    }

    #[test]
    fn test_bitmap_bit_set_out_of_range() {
        let bitmap = vec![0xff];
        assert!(!bitmap_bit_set(&bitmap, 8));
        assert!(!bitmap_bit_set(&bitmap, 1_000));
    }

    #[test]
    fn test_bitmap_bit_set_empty() {
        assert!(!bitmap_bit_set(&[], 0));
    }

    /// Mirrors on-chain `try_process_remaining_data_leaf_index` (see
    /// `programs/shred-subscription/src/processor/common.rs`) byte-for-byte:
    /// `bitmap[leaf_index / 8] |= 1 << (leaf_index % 8)` (LSB-first within
    /// the byte, via `ByteFlags::set_bit`). If the on-chain accumulate ix
    /// ever changes either the byte indexing or the bit ordering, update
    /// this helper to match — the parity test below will then fail until
    /// `bitmap_bit_set` is brought back in sync.
    fn set_leaf_accumulated_onchain_style(bitmap: &mut [u8], leaf_index: u32) {
        let byte_index = (leaf_index as usize) / 8;
        let bit_index = (leaf_index as usize) % 8;
        bitmap[byte_index] |= 1 << bit_index;
    }

    #[test]
    fn bitmap_bit_set_matches_onchain_accumulate_convention() {
        // Build a bitmap by following the on-chain accumulate steps for a
        // chosen set of leaf indices, then verify our reader sees exactly
        // those bits set. Indices are deliberately sparse across byte
        // boundaries (0, 7, 8 hit the byte-boundary edge; 64, 100, 127
        // exercise sparse high indices).
        let mut bitmap = vec![0u8; 16];
        let accumulated_indices: &[u32] = &[0, 3, 7, 8, 9, 17, 64, 100, 127];
        for &leaf_index in accumulated_indices {
            set_leaf_accumulated_onchain_style(&mut bitmap, leaf_index);
        }
        for leaf_index in 0u32..128 {
            let expected = accumulated_indices.contains(&leaf_index);
            assert_eq!(
                bitmap_bit_set(&bitmap, leaf_index as usize),
                expected,
                "leaf_index {leaf_index}"
            );
        }
    }
}
