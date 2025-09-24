use std::str::FromStr;

use anyhow::{Result, bail};
use clap::Args;
use doublezero_passport::{
    ID,
    instruction::{
        AccessMode, PassportInstructionData, SolanaValidatorAttestation,
        account::RequestAccessAccounts,
    },
    state::AccessRequest,
};
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, Wallet};
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction, offchain_message::OffchainMessage, pubkey::Pubkey,
    signature::Signature,
};

#[derive(Debug, Args)]
pub struct RequestSolanaValidatorAccessCommand {
    service_key: Pubkey,

    #[arg(long, value_name = "PUBKEY")]
    node_id: Pubkey,

    #[arg(long, short = 's', value_name = "BASE58_STRING")]
    signature: String,

    /// Offchain message version. ONLY 0 IS SUPPORTED.
    #[arg(long, value_name = "U8", default_value = "0")]
    message_version: u8,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl RequestSolanaValidatorAccessCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let RequestSolanaValidatorAccessCommand {
            service_key,
            node_id,
            signature,
            message_version,
            solana_payer_options,
        } = self;

        let verbose = solana_payer_options.signer_options.verbose;

        let wallet = Wallet::try_from(solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        let ed25519_signature = Signature::from_str(&signature)?;

        // Create attestation
        let attestation = SolanaValidatorAttestation {
            validator_id: node_id,
            service_key,
            ed25519_signature: ed25519_signature.into(),
        };

        // Verify the signature.
        let raw_message =
            AccessRequest::access_request_message(&AccessMode::SolanaValidator(attestation));

        if verbose {
            println!("Raw message: {raw_message}");
        }

        let message = OffchainMessage::new(message_version, raw_message.as_bytes())?;
        let serialized_message = message.serialize()?;

        if !ed25519_signature.verify(node_id.as_array(), &serialized_message) {
            bail!("Signature verification failed");
        } else if verbose {
            println!("Signature recovers node ID: {node_id}");
        }

        let request_access_ix = try_build_instruction(
            &ID,
            RequestAccessAccounts::new(&wallet_key, &service_key),
            &PassportInstructionData::RequestAccess(AccessMode::SolanaValidator(attestation)),
        )?;

        let mut compute_unit_limit = 10_000;

        let (_, bump) = AccessRequest::find_address(&service_key);
        compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

        let mut instructions = vec![
            request_access_ix,
            ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
        ];

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Request Solana validator access: {tx_sig}");

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
