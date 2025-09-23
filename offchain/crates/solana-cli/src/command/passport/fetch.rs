use anyhow::{Result, anyhow};
use doublezero_passport::state::ProgramConfig;
use doublezero_program_tools::zero_copy;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};

pub async fn execute_fetch(
    program_config: bool,
    solana_connection_options: SolanaConnectionOptions,
) -> Result<()> {
    let connection = SolanaConnection::try_from(solana_connection_options)?;

    if program_config {
        let program_config_key = ProgramConfig::find_address().0;
        let program_config_info = connection.get_account(&program_config_key).await?;

        let (program_config, _) =
            zero_copy::checked_from_bytes_with_discriminator::<ProgramConfig>(
                &program_config_info.data,
            )
            .ok_or(anyhow!("Failed to deserialize program config"))?;

        // TODO: Pretty print.
        println!("Program config: {program_config:?}");
    }

    Ok(())
}
