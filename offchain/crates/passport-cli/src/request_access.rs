use std::{io::Write, str::FromStr, sync::Arc};

use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_ledger_sentinel::client::solana::SolRpcClient;
use doublezero_solana_client_tools::{
    payer::{SolanaPayerOptions, SolanaSignerOptions, TransactionOutcome, Wallet},
    rpc::{SolanaConnection, SolanaConnectionOptions},
};
use doublezero_solana_sdk::{
    passport::{
        ID,
        instruction::{
            AccessMode, PassportInstructionData, SolanaValidatorAttestation,
            account::RequestAccessAccounts,
        },
        state::AccessRequest,
    },
    try_build_instruction,
};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::{
    offchain_message::OffchainMessage,
    signature::{Keypair, Signature},
};
use url::Url;

use crate::{
    access_validation::{should_continue_after_validation, validate_validator_access},
    error::{PassportCliError, Result},
    shared::SharedAccessArgs,
    util::identify_cluster,
};

#[derive(Debug, Args)]
pub struct RequestValidatorAccessArgs {
    #[command(flatten)]
    pub shared: SharedAccessArgs,
    /// Base58-encoded ed25519 signature of the access request message (service_key=AAA,backup_ids=BBBB,CCCC,DDDD)
    #[arg(long, short = 's', value_name = "BASE58_STRING")]
    pub signature: String,

    /// Continue and submit transaction even if validation fails
    #[arg(long = "force", hide = true, default_value_t = false)]
    pub force: bool,

    /// Offchain message version. ONLY 0 IS SUPPORTED.
    #[arg(long, value_name = "U8", default_value = "0")]
    pub message_version: u8,

    // --- Transaction-building knobs (per RFC-20 these are verb-owned, not
    // global connection/identity config; connection + keypair come from
    // `CliContext`). These mirror the legacy `SolanaSignerOptions` flags so the
    // offchain CLI surface is unchanged.
    /// Set the compute unit price for transaction in increments of 0.000001 lamports per compute unit.
    #[arg(long, value_name = "MICROLAMPORTS", env)]
    pub with_compute_unit_price: Option<u64>,

    /// Print verbose output.
    #[arg(
        long,
        short = 'v',
        value_name = "VERBOSE",
        default_value = "false",
        env
    )]
    pub verbose: bool,

    /// Filepath or URL to keypair to pay transaction fee.
    #[arg(long = "fee-payer", value_name = "KEYPAIR", env)]
    pub fee_payer_path: Option<String>,

    /// Simulate transaction only.
    #[arg(long, value_name = "DRY_RUN", env)]
    pub dry_run: bool,
}

impl RequestValidatorAccessArgs {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        self.run(ctx, out).await
    }

    async fn run(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        tracing::debug!(env = %ctx.env, "passport request-validator-access");

        let wallet = self.build_wallet(ctx)?;

        writeln!(out, "DoubleZero Passport - Request Validator Access")?;

        let cluster = identify_cluster(&wallet.connection).await?;
        writeln!(out, "Connected to Solana: {cluster}")?;
        writeln!(
            out,
            "\nDoubleZero Address: {}\n",
            self.shared.doublezero_address
        )?;

        let sol_client = SolRpcClient::new(
            Url::parse(&wallet.connection.url())?,
            Arc::new(Keypair::new()),
        );

        let validation_errors = validate_validator_access(
            out,
            &wallet.connection,
            &sol_client,
            &self.shared.primary_validator_id,
            &self.shared.backup_validator_ids,
            self.shared.leader_schedule_epochs,
        )
        .await?;
        if !should_continue_after_validation(out, &validation_errors, self.force)? {
            return Ok(());
        }

        let (address, _) = AccessRequest::find_address(&self.shared.doublezero_address);

        let request_account = wallet.connection.get_account(&address).await;
        if request_account.is_ok() {
            return Err(PassportCliError::AccessRequestExists(address));
        }

        let tx_sig = self.request_access(&wallet, out).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_sig {
            writeln!(out, "Request Solana validator access: {tx_sig}")?;

            wallet
                .print_verbose_output(&[tx_sig])
                .await
                .map_err(PassportCliError::other)?;
        }

        Ok(())
    }

    /// Build a `Wallet` from `CliContext` (connection URL + keypair path) plus
    /// the verb-owned transaction knobs. Reuses the shared keypair-loading and
    /// fee-payer logic in `Wallet::try_new`.
    fn build_wallet(&self, ctx: &CliContext) -> Result<Wallet> {
        // `Wallet::try_new` (and the other `Wallet` helpers below) surface
        // `anyhow::Error`; box it through `Other` so the cause chain survives.
        let opts = SolanaPayerOptions {
            connection_options: SolanaConnectionOptions::default(),
            signer_options: SolanaSignerOptions {
                keypair_path: ctx
                    .keypair_path
                    .as_ref()
                    .map(|p| p.to_string_lossy().into_owned()),
                with_compute_unit_price: self.with_compute_unit_price,
                verbose: self.verbose,
                fee_payer_path: self.fee_payer_path.clone(),
                dry_run: self.dry_run,
            },
        };
        let connection = SolanaConnection::new(ctx.solana_l1_rpc_url.clone());
        Wallet::try_new(opts, Some(connection)).map_err(PassportCliError::other)
    }

    async fn request_access(
        &self,
        wallet: &Wallet,
        out: &mut impl Write,
    ) -> Result<TransactionOutcome> {
        let ed25519_signature = Signature::from_str(&self.signature)?;
        let wallet_key = wallet.pubkey();

        let attestation = SolanaValidatorAttestation {
            validator_id: self.shared.primary_validator_id,
            service_key: self.shared.doublezero_address,
            ed25519_signature: ed25519_signature.into(),
        };

        let access_mode = if self.shared.backup_validator_ids.is_empty() {
            AccessMode::SolanaValidator(attestation)
        } else {
            AccessMode::SolanaValidatorWithBackupIds {
                attestation,
                backup_ids: self.shared.backup_validator_ids.clone(),
            }
        };

        let raw_message = AccessRequest::access_request_message(&access_mode);

        if self.verbose {
            writeln!(out, "Raw message: {raw_message}")?;
        }

        let message = OffchainMessage::new(self.message_version, raw_message.as_bytes())
            .map_err(PassportCliError::other)?;
        let serialized_message = message.serialize().map_err(PassportCliError::other)?;

        if !ed25519_signature.verify(
            self.shared.primary_validator_id.as_array(),
            &serialized_message,
        ) {
            return Err(PassportCliError::SignatureVerificationFailed);
        } else if self.verbose {
            writeln!(
                out,
                "Signature recovers node ID: {}",
                self.shared.primary_validator_id
            )?;
        }

        let request_access_ix = try_build_instruction(
            &ID,
            RequestAccessAccounts::new(&wallet_key, &self.shared.doublezero_address),
            &PassportInstructionData::RequestAccess(access_mode),
        )
        .map_err(PassportCliError::other)?;

        let (_, bump) = AccessRequest::find_address(&self.shared.doublezero_address);

        let mut compute_unit_limit = 10_000;
        compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

        let mut instructions = vec![
            request_access_ix,
            ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
        ];

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet
            .new_transaction(&instructions)
            .await
            .map_err(PassportCliError::other)?;

        wallet
            .send_or_simulate_transaction(&transaction)
            .await
            .map_err(PassportCliError::other)
    }
}
