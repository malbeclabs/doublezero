use anyhow::Result;
use clap::Args;
use doublezero_passport::instruction::AccessMode;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Args)]
pub struct FetchCommand {
    #[arg(long)]
    config: bool,

    #[arg(long, value_name = "PUBKEY")]
    access_request: Option<Pubkey>,

    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,
}

impl FetchCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let FetchCommand {
            config,
            access_request,
            solana_connection_options,
        } = self;

        let connection = SolanaConnection::try_from(solana_connection_options)?;

        if config {
            let (program_config_key, program_config) =
                super::fetch_program_config(&connection).await?;

            println!("Program config: {program_config_key}");
            println!();
            println!("Parameter                         | Value");
            println!(
                "----------------------------------+-------------------------------------------------"
            );
            println!(
                "Is program paused?                | {}",
                program_config.is_paused()
            );
            println!(
                "Is request access paused?         | {}",
                program_config.is_request_access_paused()
            );
            println!(
                "Admin key                         | {}",
                program_config.admin_key
            );
            println!(
                "Sentinel key                      | {}",
                program_config.sentinel_key
            );
            println!(
                "Request deposit lamports          | {}",
                program_config.request_deposit_lamports
            );
            println!(
                "Request fee lamports              | {}",
                program_config.request_fee_lamports
            );
            println!(
                "Solana validator backup IDs limit | {}",
                program_config.solana_validator_backup_ids_limit
            );
            println!();
        }

        // NOTE: If an access request is found, the sentinel is not doing its job.
        if let Some(access_request) = access_request {
            let (access_request_key, access_request) =
                super::fetch_access_request(&connection, &access_request).await?;

            println!("Access request: {access_request_key}");
            println!();
            match access_request {
                Some(access_request) => {
                    println!("Field                             | Value");
                    println!(
                        "----------------------------------+-------------------------------------------------"
                    );
                    println!(
                        "Service key                       | {}",
                        access_request.service_key
                    );
                    println!(
                        "Rent beneficiary key              | {}",
                        access_request.rent_beneficiary_key
                    );
                    println!(
                        "Request fee lamports              | {}",
                        access_request.request_fee_lamports
                    );
                    match access_request.checked_access_mode() {
                        Some(access_mode) => {
                            let access_mode_str = match access_mode {
                                AccessMode::SolanaValidator(_) => "Solana validator",
                                AccessMode::SolanaValidatorWithBackupIds { .. } => {
                                    "Solana validator with backup IDs"
                                }
                            };
                            println!("Access mode                       | {access_mode_str}");
                        }
                        None => {
                            println!("Access mode                       | Unknown");
                        }
                    }
                }
                None => {
                    println!("... no access request found");
                }
            }
            println!();
        }

        Ok(())
    }
}
