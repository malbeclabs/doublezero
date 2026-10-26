use anyhow::Result;
use doublezero_solana_client_tools::payer::{TransactionOutcome, Wallet};
use doublezero_solana_sdk::revenue_distribution::state::ContributorRewards;
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::{instruction::Instruction, pubkey::Pubkey};
use spl_associated_token_account_interface::{
    address::get_associated_token_address_and_bump_seed,
    instruction::create_associated_token_account_idempotent,
};
use tracing::{info, warn};

#[derive(Debug)]
pub enum LeafPayment {
    Sent,
    // Only the contributor can fix this, so the caller decides how loudly to
    // report it.
    Skipped(String),
}

/// Pay one leaf to its contributor's active recipients, creating any missing
/// recipient ATA in the same transaction. `build_distribute_instruction` gets
/// the recipient keys, whose ATAs it must list in that order.
pub async fn try_pay_leaf(
    wallet: &Wallet,
    dz_mint_key: &Pubkey,
    contributor_key: &Pubkey,
    payment_label: &str,
    build_distribute_instruction: impl FnOnce(&[&Pubkey]) -> Result<Instruction>,
) -> Result<LeafPayment> {
    const DISTRIBUTE_REWARDS_CU_BASE: u32 = 30_000;
    const PER_RECIPIENT_CU: u32 = 12_500;

    let wallet_key = wallet.pubkey();

    let (contributor_rewards_key, _) = ContributorRewards::find_address(contributor_key);

    // Fetch contributor reward recipients.
    let recipient_shares = match wallet
        .connection
        .try_fetch_zero_copy_data::<ContributorRewards>(&contributor_rewards_key)
        .await
    {
        Ok(contributor_rewards) => {
            let recipient_shares = contributor_rewards
                .recipient_shares
                .active_iter()
                .copied()
                .collect::<Vec<_>>();

            if recipient_shares.is_empty() {
                return Ok(LeafPayment::Skipped(format!(
                    "No recipients in {contributor_rewards_key} for contributor {contributor_key}"
                )));
            }

            recipient_shares
        }
        _ => {
            return Ok(LeafPayment::Skipped(format!(
                "Contributor rewards {contributor_rewards_key} not found for contributor \
                 {contributor_key}"
            )));
        }
    };

    let recipient_keys = recipient_shares
        .iter()
        .map(|share| &share.recipient_key)
        .collect::<Vec<_>>();

    let distribute_rewards_ix = build_distribute_instruction(&recipient_keys)?;

    // Derive ATA addresses together with their create-ATA compute-unit
    // estimates. The address is needed for the existence check below. The CU is
    // carried through to the recipients whose ATA must be created.
    let (ata_keys, recipient_create_compute_units) = recipient_keys
        .iter()
        .map(|recipient_key| {
            Wallet::ata_address_and_create_compute_units(recipient_key, dz_mint_key)
        })
        .unzip::<_, _, Vec<_>, Vec<_>>();

    // Build instructions to create missing ATAs. We are using idempotent just
    // in case there is a race when creating the ATAs.
    let (mut instructions, create_ata_compute_units) = wallet
        .connection
        .get_multiple_accounts(&ata_keys)
        .await?
        .into_iter()
        .zip(recipient_keys.iter())
        .zip(recipient_create_compute_units)
        .filter_map(
            |((account_info, recipient_key), create_compute_units)| match account_info {
                Some(account_info) if account_info.owner == Pubkey::default() => {
                    Some((recipient_key, create_compute_units))
                }
                None => Some((recipient_key, create_compute_units)),
                _ => None,
            },
        )
        .map(|(recipient_key, create_compute_units)| {
            let ix = create_associated_token_account_idempotent(
                &wallet_key,
                recipient_key,
                dz_mint_key,
                &spl_token_interface::ID,
            );

            (ix, create_compute_units)
        })
        .unzip::<_, _, Vec<_>, Vec<_>>();

    if !instructions.is_empty() {
        warn!("Creating {} ATAs", instructions.len());
    }

    instructions.push(distribute_rewards_ix);

    // Add simple memo to indicate that distributing rewards was relayed.
    let (memo_ix, memo_compute_units) = Wallet::build_memo_instruction_with_compute_units(b"Relay");
    instructions.push(memo_ix);

    // The program derives every recipient's ATA, trying bumps down from 255, so
    // each bump past the first costs more than `PER_RECIPIENT_CU` covers.
    let derive_ata_compute_units = recipient_keys
        .iter()
        .map(|recipient_key| {
            let (_, bump) = get_associated_token_address_and_bump_seed(
                recipient_key,
                dz_mint_key,
                &spl_associated_token_account_interface::program::ID,
                &spl_token_interface::ID,
            );
            Wallet::compute_units_for_bump_seed(bump)
        })
        .sum::<u32>();

    let compute_unit_limit = DISTRIBUTE_REWARDS_CU_BASE
        + recipient_keys.len() as u32 * PER_RECIPIENT_CU
        + derive_ata_compute_units
        + create_ata_compute_units.iter().sum::<u32>()
        + memo_compute_units;

    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

    if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
        info!("Distribute rewards for {payment_label}: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(LeafPayment::Sent)
}
