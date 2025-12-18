use anyhow::{Context, Result};
use doublezero_solana_client_tools::rpc::SolanaConnection;
use solana_sdk::pubkey::Pubkey;

use super::state::ProgramConfig;

pub async fn try_fetch_config(
    connection: &SolanaConnection,
) -> Result<(Pubkey, Box<ProgramConfig>)> {
    let (program_config_key, _) = ProgramConfig::find_address();

    let program_config = connection
        .try_fetch_zero_copy_data::<ProgramConfig>(&program_config_key)
        .await
        .context("Revenue Distribution program not initialized")?;
    Ok((program_config_key, program_config.mucked_data))
}
