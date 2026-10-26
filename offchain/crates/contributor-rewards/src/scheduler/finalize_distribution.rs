use anyhow::{Context, Result};
use doublezero_solana_client_tools::payer::{TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    revenue_distribution::{
        ID,
        instruction::{
            RevenueDistributionInstructionData,
            account::{FinalizeDistributionDebtAccounts, FinalizeDistributionRewardsAccounts},
        },
        state::{Distribution, ProgramConfig},
        types::DoubleZeroEpoch,
    },
    try_build_instruction,
};
use solana_compute_budget_interface::ComputeBudgetInstruction;

use super::pause_gate::is_config_paused;

pub async fn try_finalize_distribution(wallet: &Wallet, dz_epoch_value: u64) -> Result<()> {
    let dz_epoch = DoubleZeroEpoch::new(dz_epoch_value);

    let config = wallet
        .connection
        .try_fetch_zero_copy_data::<ProgramConfig>(&ProgramConfig::find_address().0)
        .await?;

    if is_config_paused(&config) {
        return Ok(());
    }

    let distribution = wallet
        .connection
        .try_fetch_zero_copy_data::<Distribution>(&Distribution::find_address(dz_epoch).0)
        .await?;

    if distribution.is_rewards_calculation_finalized() {
        tracing::debug!("Rewards of DZ epoch {dz_epoch} are already finalized");
        return Ok(());
    }

    // The program finalizes a null root when nothing is owed, and then nobody can
    // post a root for the epoch.
    if distribution.rewards_merkle_root == Default::default() {
        tracing::warn!("DZ epoch {dz_epoch} has no rewards root yet, so it is not finalized");
        return Ok(());
    }

    let minimum_epoch_duration_to_finalize_rewards = config
        .checked_minimum_epoch_duration_to_finalize_rewards()
        .context("Minimum epoch duration to finalize rewards not set")?;
    let finalizable_dz_epoch =
        dz_epoch.saturating_add_duration(minimum_epoch_duration_to_finalize_rewards);

    if finalizable_dz_epoch > config.next_completed_dz_epoch {
        tracing::warn!(
            "DZ epoch {dz_epoch} can be finalized from DZ epoch {finalizable_dz_epoch}, and the program is at {}",
            config.next_completed_dz_epoch
        );
        return Ok(());
    }

    let wallet_key = wallet.pubkey();

    let mut instructions = vec![];
    let mut compute_unit_limit = 5_000;

    if !distribution.is_debt_calculation_finalized() {
        // The debt calculation is not finalized when no debt was calculated.
        // Finalizing it must be done before the rewards can be finalized.
        if distribution.solana_validator_debt_merkle_root != Default::default() {
            tracing::warn!(
                "DZ epoch {dz_epoch} has a debt root that is not finalized, so it is not finalized"
            );
            return Ok(());
        }

        tracing::warn!("Finalizing debt calculation for epoch {dz_epoch} with zero debt");
        let finalize_debt_ix = try_build_instruction(
            &ID,
            FinalizeDistributionDebtAccounts::new(&wallet_key, dz_epoch, &wallet_key),
            &RevenueDistributionInstructionData::FinalizeDistributionDebt,
        )?;
        instructions.push(finalize_debt_ix);
        compute_unit_limit += 5_000;
    }

    let finalize_rewards_ix = try_build_instruction(
        &ID,
        FinalizeDistributionRewardsAccounts::new(&wallet_key, dz_epoch),
        &RevenueDistributionInstructionData::FinalizeDistributionRewards,
    )?;
    instructions.push(finalize_rewards_ix);
    compute_unit_limit += 7_500;

    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let TransactionOutcome::Executed(tx_sig) = tx_sig {
        tracing::info!("Finalize distribution rewards for epoch {dz_epoch}: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}
