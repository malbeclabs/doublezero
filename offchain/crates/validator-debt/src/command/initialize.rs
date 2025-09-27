use anyhow::{Result, bail, ensure};
use clap::Args;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    ID as REVENUE_DISTRIBUTION_PROGRAM_ID,
    instruction::{RevenueDistributionInstructionData, account::InitializeDistributionAccounts},
    state::{self, Distribution, ProgramConfig},
};
use doublezero_scheduled_command::{Schedulable, ScheduleOption};
use doublezero_solana_client_tools::{
    log_info, log_warn,
    payer::{SolanaPayerOptions, Wallet},
    rpc::DoubleZeroLedgerConnectionOptions,
    zero_copy::ZeroCopyAccountOwned,
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{
    commitment_config::CommitmentConfig, compute_budget::ComputeBudgetInstruction, signer::Signer,
};

#[derive(Debug, Args, Clone)]
pub struct InitializeDistributionCommand {
    #[command(flatten)]
    schedule_or_force: super::ScheduleOrForce,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,

    #[command(flatten)]
    dz_ledger_connection_options: DoubleZeroLedgerConnectionOptions,
}

#[async_trait::async_trait]
impl Schedulable for InitializeDistributionCommand {
    fn schedule(&self) -> &ScheduleOption {
        &self.schedule_or_force.schedule
    }

    async fn execute_once(&self) -> Result<()> {
        self.schedule_or_force.ensure_safe_execution()?;

        let mut wallet = Wallet::try_from(self.solana_payer_options.clone())?;
        wallet.connection.cache_if_mainnet().await?;

        let (next_dz_epoch, expected_accountant_key) =
            ZeroCopyAccountOwned::<ProgramConfig>::from_rpc_client(
                &wallet.connection,
                &ProgramConfig::find_address().0,
            )
            .await
            .map(|config| (config.data.next_dz_epoch, config.data.debt_accountant_key))?;

        ensure!(
            wallet.signer.pubkey() == expected_accountant_key,
            "Signer does not match expected debt accountant"
        );

        let dz_ledger_rpc_client = RpcClient::new_with_commitment(
            self.dz_ledger_connection_options.dz_ledger_url.clone(),
            CommitmentConfig::confirmed(),
        );

        super::ensure_same_network_environment(&dz_ledger_rpc_client, wallet.connection.is_mainnet)
            .await?;

        // We want to make sure the next DZ epoch is in sync with the last
        // completed DZ epoch.
        let expected_completed_dz_epoch = dz_ledger_rpc_client.get_epoch_info().await?.epoch - 1;

        // Ensure that the epoch from the DoubleZero Ledger network equals the next
        // one known by the Revenue Distribution program. If it does not, this
        // method has not been called for a long time.
        if next_dz_epoch.value() != expected_completed_dz_epoch {
            let err_msg = format!(
                "Last completed DZ epoch {expected_completed_dz_epoch} != program's epoch {next_dz_epoch}"
            );

            // If the force flag is set, only allow the command to play catch up
            // if the next DZ epoch is less than the expected completed DZ
            // epoch. Prompt to be extra sure.
            if self.schedule_or_force.force && next_dz_epoch.value() < expected_completed_dz_epoch {
                log_warn!(err_msg);
                super::proceed_prompt()?;
            // If the schedule flag is set, simply warn so we do not spam any
            // monitoring system.
            } else if self.schedule_or_force.schedule.is_scheduled() {
                log_warn!(err_msg);

                return Ok(());
            // Otherwise, we should not be allowed to proceed.
            } else {
                bail!("{err_msg}");
            }
        }

        let dz_mint_key = if wallet.connection.is_mainnet {
            doublezero_revenue_distribution::env::mainnet::DOUBLEZERO_MINT_KEY
        } else {
            doublezero_revenue_distribution::env::development::DOUBLEZERO_MINT_KEY
        };

        let initialize_distribution_ix = try_build_instruction(
            &REVENUE_DISTRIBUTION_PROGRAM_ID,
            InitializeDistributionAccounts::new(
                &expected_accountant_key,
                &expected_accountant_key,
                next_dz_epoch,
                &dz_mint_key,
            ),
            &RevenueDistributionInstructionData::InitializeDistribution,
        )
        .unwrap();

        let mut compute_unit_limit = 24_000;

        let (distribution_key, bump) = Distribution::find_address(next_dz_epoch);
        compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

        let (_, bump) = state::find_2z_token_pda_address(&distribution_key);
        compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

        let instructions = vec![
            initialize_distribution_ix,
            ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
            ComputeBudgetInstruction::set_compute_unit_price(1_000_000), // Land it.
        ];

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            log_info!("Initialize distribution: {tx_sig}");

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
