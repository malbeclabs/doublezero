use std::sync::Arc;

use anyhow::Result;
use clap::Args;
use doublezero_ledger_sentinel::client::solana::SolRpcClient;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};
use doublezero_solana_sdk::passport::{
    instruction::{AccessMode, SolanaValidatorAttestation},
    state::AccessRequest,
};
use solana_sdk::signature::Keypair;
use url::Url;

use super::{
    SharedAccessArgs,
    access_validation::{should_continue_after_validation, validate_validator_access},
};
use crate::utils::identify_cluster;

/*
   doublezero-solana passport request-access --doublezero-address SSSS --primary-validator-id AAA --backup-validator-ids BBB,CCC --signature XXXXX
*/

#[derive(Debug, Args)]
pub struct PrepareValidatorAccessCommand {
    #[command(flatten)]
    shared: SharedAccessArgs,

    #[arg(long, default_value_t = false)]
    force: bool,

    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,
}

impl PrepareValidatorAccessCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let PrepareValidatorAccessCommand {
            shared:
                SharedAccessArgs {
                    doublezero_address,
                    primary_validator_id,
                    backup_validator_ids,
                    leader_schedule_epochs,
                },
            solana_connection_options,
            force,
        } = self;

        // Establish a connection to the Solana cluster
        let connection = SolanaConnection::from(solana_connection_options);
        let sol_client = SolRpcClient::new(
            Url::parse(&connection.url()).unwrap(),
            Arc::new(Keypair::new()),
        );

        // Identify the cluster
        let cluster = identify_cluster(&connection).await;
        // Fetch the cluster nodes
        println!("DoubleZero Passport - Prepare Validator Access Request");
        println!("Connected to Solana: {:}", cluster);

        println!("\nDoubleZero Address: {doublezero_address}\n");

        let errors = validate_validator_access(
            &connection,
            &sol_client,
            &primary_validator_id,
            &backup_validator_ids,
            leader_schedule_epochs,
        )
        .await?;
        if !should_continue_after_validation(&errors, force) {
            return Ok(());
        }

        println!(
            "\n\nTo request access, sign the following message with your validator's identity key:\n"
        );

        // Create attestation
        let attestation = SolanaValidatorAttestation {
            validator_id: primary_validator_id,
            service_key: doublezero_address,
            ed25519_signature: [0u8; 64],
        };

        // Verify the signature.
        let raw_message = if backup_validator_ids.is_empty() {
            AccessRequest::access_request_message(&AccessMode::SolanaValidator(attestation))
        } else {
            AccessRequest::access_request_message(&AccessMode::SolanaValidatorWithBackupIds {
                attestation,
                backup_ids: backup_validator_ids.clone(),
            })
        };

        println!(
            "solana sign-offchain-message \\\n   {raw_message} \\\n   -k <identity-keypair-file.json>\n"
        );

        Ok(())
    }
}
