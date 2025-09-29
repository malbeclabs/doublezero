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

/*
   doublezero-solana passport request-access --doublezero-address SSSS --primary-validator-id AAA --backup-validator-ids BBB,CCC --signature XXXXX
*/

#[derive(Debug, Args)]
pub struct RequestValidatorAccessCommand {
    /// The DoubleZero service key to request access from
    #[arg(long)]
    doublezero_address: Pubkey,
    /// The validator's node ID (identity pubkey)
    #[arg(long, value_name = "PUBKEY")]
    primary_validator_id: Pubkey,
    /// Optional backup validator IDs (identity pubkeys)
    #[arg(long, value_name = "PUBKEY,PUBKEY,PUBKEY", value_delimiter = ',')]
    backup_validator_ids: Vec<Pubkey>,
    /// Base58-encoded ed25519 signature of the access request message (service_key=AAA,backup_ids=BBBB,CCCC,DDDD)
    #[arg(long, short = 's', value_name = "BASE58_STRING")]
    signature: String,

    /// Offchain message version. ONLY 0 IS SUPPORTED.
    #[arg(long, value_name = "U8", default_value = "0")]
    message_version: u8,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl RequestValidatorAccessCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let wallet = Wallet::try_from(self.solana_payer_options.clone())?;

        let (address, _) = AccessRequest::find_address(&self.doublezero_address);

        let request_account = wallet.connection.get_account(&address).await;
        if request_account.is_ok() {
            bail!("Access request already exists: {address}");
        }

        let tx_sig = self.request_access(&wallet).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Request Solana validator access: {tx_sig}");

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }

    async fn request_access(&self, wallet: &Wallet) -> Result<Option<Signature>> {
        let wallet_key = wallet.pubkey();
        let ed25519_signature = Signature::from_str(&self.signature)?;

        // Create attestation
        let attestation = SolanaValidatorAttestation {
            validator_id: self.primary_validator_id,
            service_key: self.doublezero_address,
            ed25519_signature: ed25519_signature.into(),
        };

        // Verify the signature.
        let raw_message = if self.backup_validator_ids.is_empty() {
            AccessRequest::access_request_message(&AccessMode::SolanaValidator(attestation))
        } else {
            AccessRequest::access_request_message(&AccessMode::SolanaValidatorWithBackupIds {
                attestation,
                backup_ids: self.backup_validator_ids.clone(),
            })
        };

        if self.solana_payer_options.signer_options.verbose {
            println!("Raw message: {raw_message}");
        }

        let message = OffchainMessage::new(self.message_version, raw_message.as_bytes())?;
        let serialized_message = message.serialize()?;

        if !ed25519_signature.verify(self.primary_validator_id.as_array(), &serialized_message) {
            bail!("Signature verification failed");
        } else if self.solana_payer_options.signer_options.verbose {
            println!("Signature recovers node ID: {}", self.primary_validator_id);
        }

        let request_access_ix = try_build_instruction(
            &ID,
            RequestAccessAccounts::new(&wallet_key, &self.doublezero_address),
            &PassportInstructionData::RequestAccess(AccessMode::SolanaValidator(attestation)),
        )?;

        let (_, bump) = AccessRequest::find_address(&self.doublezero_address);

        let mut compute_unit_limit = 10_000;
        compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

        let mut instructions = vec![
            request_access_ix,
            ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
        ];

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;

        wallet.send_or_simulate_transaction(&transaction).await
    }
}
