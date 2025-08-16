use doublezero_passport::{instruction::AccessMode, state::AccessRequest};
use solana_sdk::{
    hash::Hash,
    instruction::Instruction,
    message::{v0::Message, VersionedMessage},
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

pub fn verify_access_request(
    &AccessMode::SolanaValidator {
        ed25519_signature,
        service_key,
        validator_id,
    }: &AccessMode,
) -> Result<()> {
    let message = AccessRequest::access_request_message(&service_key);
    let signature: Signature = ed25519_signature.into();

    if !signature.verify(validator_id.as_array(), &message) {
        return Err(Error::SignatureVerify);
    }

    Ok(())
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
    use solana_sdk::{pubkey::Pubkey, signature::Keypair, signer::Signer};

    #[test]
    fn test_signature_verification() {
        let service_key = Pubkey::new_unique();
        let validator_id = Keypair::new();

        let message = AccessRequest::access_request_message(&service_key);
        let signature_bytes: [u8; 64] = validator_id.sign_message(&message).into();

        let access_mode = AccessMode::SolanaValidator {
            validator_id: validator_id.pubkey(),
            service_key,
            ed25519_signature: signature_bytes,
        };

        assert!(verify_access_request(&access_mode).is_ok());
    }
}
