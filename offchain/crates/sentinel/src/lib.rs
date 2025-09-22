use doublezero_passport::{instruction::AccessMode, state::AccessRequest};
use solana_sdk::{
    hash::Hash,
    instruction::Instruction,
    message::{VersionedMessage, v0::Message},
    offchain_message::OffchainMessage,
    pubkey::Pubkey,
    signature::{Keypair, Signature},
    signer::Signer,
    transaction::VersionedTransaction,
};

pub mod client;
mod error;
pub mod sentinel;
pub mod settings;

pub use error::{Error, Result};

#[derive(Debug)]
pub struct AccessIds {
    request_pda: Pubkey,
    rent_beneficiary_key: Pubkey,
    mode: AccessMode,
}

// Verify access request by and return validator_id (pubkey) if successful
pub fn verify_access_request(access_mode: &AccessMode) -> Result<Pubkey> {
    const OFFCHAIN_MSG_SUPPORTED_VSN: u8 = 0;

    let raw_message = AccessRequest::access_request_message(access_mode);
    let offchain_msg = OffchainMessage::new(OFFCHAIN_MSG_SUPPORTED_VSN, raw_message.as_bytes())?;
    let serialized_msg = offchain_msg.serialize()?;

    // Get the attestation
    let attestation = match access_mode {
        AccessMode::SolanaValidator(attestation) => attestation,
        AccessMode::SolanaValidatorWithBackupIds { attestation, .. } => attestation,
    };

    // Get signature from attestation
    let signature: Signature = attestation.ed25519_signature.into();

    if !signature.verify(attestation.validator_id.as_array(), &serialized_msg) {
        return Err(Error::SignatureVerify);
    }

    Ok(attestation.validator_id)
}

pub fn new_transaction(
    instructions: &[Instruction],
    signers: &[&Keypair],
    recent_blockhash: Hash,
) -> VersionedTransaction {
    let message =
        Message::try_compile(&signers[0].pubkey(), instructions, &[], recent_blockhash).unwrap();

    VersionedTransaction::try_new(VersionedMessage::V0(message), signers).unwrap()
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_passport::instruction::SolanaValidatorAttestation;
    use solana_sdk::{pubkey::Pubkey, signature::Keypair, signer::Signer};

    #[test]
    fn test_signature_verification() {
        let service_key = Pubkey::new_unique();
        let validator_id = Keypair::new();

        // Create mutable attestation first to satisfy the access_request_message input type
        // We will overwrite the signature later before verification
        let mut attestation = SolanaValidatorAttestation {
            validator_id: validator_id.pubkey(),
            service_key,
            ed25519_signature: [0; 64],
        };

        let raw_message =
            AccessRequest::access_request_message(&AccessMode::SolanaValidator(attestation));
        let offchain_msg = OffchainMessage::new(0u8, raw_message.as_bytes()).unwrap();
        let signature_bytes: [u8; 64] = validator_id
            .sign_message(&offchain_msg.serialize().unwrap())
            .into();

        // overwrite the signature
        attestation.ed25519_signature = signature_bytes;

        let access_mode = AccessMode::SolanaValidator(attestation);
        assert_eq!(
            verify_access_request(&access_mode).unwrap(),
            validator_id.pubkey()
        );
    }

    #[test]
    fn test_signature_verification_with_backup_ids() {
        let service_key = Pubkey::new_unique();
        let validator_id = Keypair::new();
        let backup_id_1 = Pubkey::new_unique();
        let backup_id_2 = Pubkey::new_unique();

        // Create mutable attestation
        let mut attestation = SolanaValidatorAttestation {
            validator_id: validator_id.pubkey(),
            service_key,
            ed25519_signature: [0; 64],
        };

        // Create access mode with backup IDs
        let access_mode_for_msg = AccessMode::SolanaValidatorWithBackupIds {
            attestation,
            backup_ids: vec![backup_id_1, backup_id_2],
        };

        let raw_message = AccessRequest::access_request_message(&access_mode_for_msg);
        let offchain_msg = OffchainMessage::new(0u8, raw_message.as_bytes()).unwrap();
        let signature_bytes: [u8; 64] = validator_id
            .sign_message(&offchain_msg.serialize().unwrap())
            .into();

        // Update signature
        attestation.ed25519_signature = signature_bytes;

        let access_mode = AccessMode::SolanaValidatorWithBackupIds {
            attestation,
            backup_ids: vec![backup_id_1, backup_id_2],
        };
        assert_eq!(
            verify_access_request(&access_mode).unwrap(),
            validator_id.pubkey()
        );
    }

    #[test]
    fn test_signature_verification_failure() {
        let service_key = Pubkey::new_unique();
        let validator_id = Keypair::new();
        let wrong_keypair = Keypair::new();

        let mut attestation = SolanaValidatorAttestation {
            validator_id: validator_id.pubkey(),
            service_key,
            ed25519_signature: [0; 64],
        };

        let raw_message =
            AccessRequest::access_request_message(&AccessMode::SolanaValidator(attestation));
        let offchain_msg = OffchainMessage::new(0u8, raw_message.as_bytes()).unwrap();
        // Sign with wrong keypair
        let signature_bytes: [u8; 64] = wrong_keypair
            .sign_message(&offchain_msg.serialize().unwrap())
            .into();

        attestation.ed25519_signature = signature_bytes;

        let access_mode = AccessMode::SolanaValidator(attestation);
        assert!(verify_access_request(&access_mode).is_err());
    }
}
