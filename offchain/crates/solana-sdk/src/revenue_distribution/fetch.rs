use anyhow::{Context, Result, ensure};
use borsh::BorshDeserialize;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData, rpc::SolanaConnection,
};
use solana_sdk::pubkey::Pubkey;

use super::{
    state::{Distribution, Journal, ProgramConfig},
    types::DoubleZeroEpoch,
};
use crate::sol_conversion::state::{
    ConfigurationRegistry as SolConversionConfigurationRegistry, FillsRegistry,
    ProgramState as SolConversionProgramState,
};

pub async fn try_fetch_config(
    connection: &SolanaConnection,
) -> Result<(Pubkey, Box<ProgramConfig>)> {
    let (program_config_key, _) = ProgramConfig::find_address();

    let program_config = connection
        .try_fetch_zero_copy_data(&program_config_key)
        .await
        .context("Revenue Distribution program not initialized")?;
    Ok((program_config_key, program_config.mucked_data))
}

pub async fn try_fetch_distribution(
    connection: &SolanaConnection,
    dz_epoch_value: u64,
) -> Result<(Pubkey, ZeroCopyAccountOwnedData<Distribution>)> {
    let dz_epoch = DoubleZeroEpoch::new(dz_epoch_value);
    let (distribution_key, _) = Distribution::find_address(dz_epoch);

    let distribution = connection
        .try_fetch_zero_copy_data(&distribution_key)
        .await
        .with_context(|| format!("Distribution not found for epoch {dz_epoch}"))?;
    Ok((distribution_key, distribution))
}

pub struct SolConversionState {
    pub program_state: (Pubkey, Box<SolConversionProgramState>),
    pub configuration_registry: (Pubkey, Box<SolConversionConfigurationRegistry>),
    pub journal: (Pubkey, ZeroCopyAccountOwnedData<Journal>),
    pub fixed_fill_quantity: u64,
}

impl SolConversionState {
    pub async fn try_fetch(connection: &SolanaConnection) -> Result<Self> {
        const FAILED_FETCH_ERROR: &str = "SOL Conversion program not initialized";

        let (program_state_key, _) = SolConversionProgramState::find_address();
        let (configuration_registry_key, _) = SolConversionConfigurationRegistry::find_address();
        let (journal_key, _) = Journal::find_address();

        let account_infos = connection
            .get_multiple_accounts(&[program_state_key, configuration_registry_key, journal_key])
            .await
            .context(FAILED_FETCH_ERROR)?
            .into_iter()
            .flatten()
            .collect::<Vec<_>>();
        ensure!(account_infos.len() == 3, FAILED_FETCH_ERROR);

        let program_state_data = Box::<_>::deserialize(&mut &account_infos[0].data[8..])?;

        // Type is not known at compile time for some reason.
        let configuration_registry_data = Box::<SolConversionConfigurationRegistry>::deserialize(
            &mut &account_infos[1].data[8..],
        )?;

        let journal_data = ZeroCopyAccountOwnedData::from_account(&account_infos[2])
            .context("Revenue Distribution program not initialized")?;

        let fixed_fill_quantity = configuration_registry_data.fixed_fill_quantity;

        Ok(Self {
            program_state: (program_state_key, program_state_data),
            configuration_registry: (configuration_registry_key, configuration_registry_data),
            journal: (journal_key, journal_data),
            fixed_fill_quantity,
        })
    }

    pub async fn try_fetch_fill_registry(
        &self,
        connection: &SolanaConnection,
    ) -> Result<(Pubkey, ZeroCopyAccountOwnedData<FillsRegistry>)> {
        let fill_registry_key = self.program_state.1.fills_registry_key;
        let fill_registry = connection
            .try_fetch_zero_copy_data(&fill_registry_key)
            .await?;
        Ok((fill_registry_key, fill_registry))
    }
}
