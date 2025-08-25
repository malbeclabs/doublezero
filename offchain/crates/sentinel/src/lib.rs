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

pub fn verify_access_request(
    &AccessMode::SolanaValidator {
        ed25519_signature,
        service_key,
        validator_id,
    }: &AccessMode,
) -> Result<()> {
    const OFFCHAIN_MSG_SUPPORTED_VSN: u8 = 0;

    let raw_message = AccessRequest::access_request_message(&service_key);
    let offchain_msg = OffchainMessage::new(OFFCHAIN_MSG_SUPPORTED_VSN, raw_message.as_bytes())?;
    let serialized_msg = offchain_msg.serialize()?;

    let signature: Signature = ed25519_signature.into();

    if !signature.verify(validator_id.as_array(), &serialized_msg) {
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

        let raw_message = AccessRequest::access_request_message(&service_key);
        let offchain_msg = OffchainMessage::new(0u8, raw_message.as_bytes()).unwrap();
        let signature_bytes: [u8; 64] = validator_id
            .sign_message(&offchain_msg.serialize().unwrap())
            .into();

        let access_mode = AccessMode::SolanaValidator {
            validator_id: validator_id.pubkey(),
            service_key,
            ed25519_signature: signature_bytes,
        };

        assert!(verify_access_request(&access_mode).is_ok());
    }
}
