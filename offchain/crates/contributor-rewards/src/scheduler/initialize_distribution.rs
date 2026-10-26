use anyhow::{Context, Result, ensure};
use doublezero_solana_client_tools::{
    payer::{TransactionOutcome, Wallet},
    rpc::DoubleZeroLedgerConnection,
};
use doublezero_solana_sdk::{
    environment_2z_token_mint_key,
    revenue_distribution::{
        ID,
        instruction::{
            RevenueDistributionInstructionData,
            account::{
                FinalizeDistributionDebtAccounts, FinalizeDistributionRewardsAccounts,
                InitializeDistributionAccounts,
            },
        },
        state::{self, Distribution, ProgramConfig},
        types::DoubleZeroEpoch,
    },
    try_build_instruction,
};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::signer::Signer;

use super::pause_gate::is_config_paused;

pub async fn try_initialize_distribution(
    wallet: &Wallet,
    dz_connection: &DoubleZeroLedgerConnection,
) -> Result<()> {
    let network_env = wallet.connection.try_network_environment().await?;

    let config = wallet
        .connection
        .try_fetch_zero_copy_data::<ProgramConfig>(&ProgramConfig::find_address().0)
        .await?;

    if is_config_paused(&config) {
        return Ok(());
    }

    ensure!(
        wallet.signer.pubkey() == config.debt_accountant_key,
        "Signer does not match expected debt accountant"
    );

    let next_dz_epoch = config.next_completed_dz_epoch;

    // We want to make sure the next DZ epoch is in sync with the last
    // completed DZ epoch.
    let expected_completed_dz_epoch = dz_connection
        .get_epoch_info()
        .await?
        .epoch
        .saturating_sub(1);

    // Ensure that the epoch from the DoubleZero Ledger network equals
    // the next one known by the Revenue Distribution program.
    if next_dz_epoch.value() != expected_completed_dz_epoch {
        tracing::warn!(
            "Last completed DZ epoch {expected_completed_dz_epoch} != program's epoch {next_dz_epoch}"
        );
        return Ok(());
    }

    let minimum_epoch_duration_to_finalize_rewards = config
        .checked_minimum_epoch_duration_to_finalize_rewards()
        .context("Minimum epoch duration to finalize rewards not set")?;
    let rewards_dz_epoch = DoubleZeroEpoch::new(
        next_dz_epoch
            .value()
            .saturating_sub(minimum_epoch_duration_to_finalize_rewards.into())
            .saturating_add(1),
    );

    let rewards_distribution = wallet
        .connection
        .try_fetch_zero_copy_data::<Distribution>(&Distribution::find_address(rewards_dz_epoch).0)
        .await?;

    let wallet_key = wallet.pubkey();
    let dz_mint_key = environment_2z_token_mint_key(network_env);

    let initialize_distribution_ix = try_build_instruction(
        &ID,
        InitializeDistributionAccounts::new(&wallet_key, &wallet_key, next_dz_epoch, &dz_mint_key),
        &RevenueDistributionInstructionData::InitializeDistribution,
    )
    .unwrap();

    let mut compute_unit_limit = 75_000;

    let (distribution_key, bump) = Distribution::find_address(next_dz_epoch);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let (_, bump) = state::find_2z_token_pda_address(&distribution_key);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let mut instructions = vec![initialize_distribution_ix];

    let has_zero_debt = has_zero_distribution_debt(&rewards_distribution);

    if rewards_distribution.is_debt_calculation_finalized() || has_zero_debt {
        // The debt calculation may not have been finalized yet if there was no
        // debt calculated. Finalizing must be done before rewards can be
        // distributed.
        if has_zero_debt {
            tracing::warn!(
                "Finalizing debt calculation for epoch {rewards_dz_epoch} with zero debt"
            );
            let finalize_debt_ix = try_build_instruction(
                &ID,
                FinalizeDistributionDebtAccounts::new(&wallet_key, rewards_dz_epoch, &wallet_key),
                &RevenueDistributionInstructionData::FinalizeDistributionDebt,
            )?;
            instructions.push(finalize_debt_ix);
            compute_unit_limit += 5_000;
        }

        if !rewards_distribution.is_rewards_calculation_finalized() {
            let finalize_rewards_ix = try_build_instruction(
                &ID,
                FinalizeDistributionRewardsAccounts::new(&wallet_key, rewards_dz_epoch),
                &RevenueDistributionInstructionData::FinalizeDistributionRewards,
            )?;
            instructions.push(finalize_rewards_ix);
            compute_unit_limit += 7_500;
        }
    }

    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    // The price is fixed, so that we land the distribution initialization.
    instructions.push(ComputeBudgetInstruction::set_compute_unit_price(100_000));

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let TransactionOutcome::Executed(tx_sig) = tx_sig {
        tracing::info!("Initialize distribution: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

#[inline(always)]
fn has_zero_distribution_debt(rewards_distribution: &Distribution) -> bool {
    rewards_distribution.solana_validator_debt_merkle_root == Default::default()
}
