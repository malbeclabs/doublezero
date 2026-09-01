use std::{io::Write, sync::Arc};

use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_ledger_sentinel::client::solana::SolRpcClient;
use doublezero_solana_client_tools::rpc::SolanaConnection;
use doublezero_solana_sdk::passport::{
    instruction::{AccessMode, SolanaValidatorAttestation},
    state::AccessRequest,
};
use solana_sdk::signature::Keypair;
use url::Url;

use crate::{
    access_validation::{should_continue_after_validation, validate_validator_access},
    error::Result,
    shared::SharedAccessArgs,
    util::identify_cluster,
};

#[derive(Debug, Args)]
pub struct PrepareValidatorAccessArgs {
    #[command(flatten)]
    pub shared: SharedAccessArgs,

    #[arg(long, default_value_t = false)]
    pub force: bool,
}

impl PrepareValidatorAccessArgs {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        self.run(ctx, out).await
    }

    async fn run(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        tracing::debug!(env = %ctx.env, "passport prepare-validator-access");

        let SharedAccessArgs {
            doublezero_address,
            primary_validator_id,
            backup_validator_ids,
            leader_schedule_epochs,
        } = self.shared;

        let connection = SolanaConnection::new(ctx.solana_l1_rpc_url.clone());
        let sol_client =
            SolRpcClient::new(Url::parse(&connection.url())?, Arc::new(Keypair::new()));

        let cluster = identify_cluster(&connection).await?;
        writeln!(
            out,
            "DoubleZero Passport - Prepare Validator Access Request"
        )?;
        writeln!(out, "Connected to Solana: {cluster}")?;
        writeln!(out, "\nDoubleZero Address: {doublezero_address}\n")?;

        let errors = validate_validator_access(
            out,
            &connection,
            &sol_client,
            &primary_validator_id,
            &backup_validator_ids,
            leader_schedule_epochs,
        )
        .await?;
        if !should_continue_after_validation(out, &errors, self.force)? {
            return Ok(());
        }

        writeln!(
            out,
            "\n\nTo request access, sign the following message with your validator's identity key:\n"
        )?;

        let attestation = SolanaValidatorAttestation {
            validator_id: primary_validator_id,
            service_key: doublezero_address,
            ed25519_signature: [0u8; 64],
        };

        let raw_message = if backup_validator_ids.is_empty() {
            AccessRequest::access_request_message(&AccessMode::SolanaValidator(attestation))
        } else {
            AccessRequest::access_request_message(&AccessMode::SolanaValidatorWithBackupIds {
                attestation,
                backup_ids: backup_validator_ids.clone(),
            })
        };

        writeln!(
            out,
            "solana sign-offchain-message \\\n   {raw_message} \\\n   -k <identity-keypair-file.json>\n"
        )?;

        Ok(())
    }
}
