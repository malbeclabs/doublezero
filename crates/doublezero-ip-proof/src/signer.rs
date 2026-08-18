//! Off-chain signing and verification, behind the `signer` feature.
//!
//! The serviceability program does not use this module: onchain, the signature is checked by the
//! native Ed25519 precompile and the program only reconstructs [`signed_message_for`] and
//! compares it against what that instruction covers.

use crate::{signed_message_for, IpOwnershipProof, IpProofError, IP_PROOF_VERSION};
use solana_keypair::Keypair;
use solana_program::pubkey::Pubkey;
use solana_signature::Signature;
use solana_signer::Signer;
use std::net::Ipv4Addr;

/// Issues a proof at [`IP_PROOF_VERSION`], signed by the verifier keypair.
pub fn sign(
    verifier: &Keypair,
    payer: &Pubkey,
    client_ip: &Ipv4Addr,
    epoch: u64,
    user_type: u8,
) -> IpOwnershipProof {
    sign_version(
        IP_PROOF_VERSION,
        verifier,
        payer,
        client_ip,
        epoch,
        user_type,
    )
}

/// Issues a proof at an explicit layout version. Only needed while two versions are in flight; new
/// issuers want [`sign`].
pub fn sign_version(
    version: u8,
    verifier: &Keypair,
    payer: &Pubkey,
    client_ip: &Ipv4Addr,
    epoch: u64,
    user_type: u8,
) -> IpOwnershipProof {
    let signature = verifier.sign_message(&signed_message_for(
        version, payer, client_ip, epoch, user_type,
    ));

    IpOwnershipProof {
        version,
        payer: *payer,
        client_ip: *client_ip,
        epoch,
        user_type,
        signature: signature.into(),
    }
}

/// Checks the proof's signature against the verifier public key.
///
/// This says nothing about freshness, about whether the proof's version is supported, or about
/// whether the proof matches the operation it is being presented for — the caller still checks
/// [`crate::is_supported_version`], the epoch window, the payer, the client IP, and the user type.
pub fn verify(proof: &IpOwnershipProof, verifier_pubkey: &Pubkey) -> Result<(), IpProofError> {
    let signature = Signature::from(proof.signature);

    if signature.verify(verifier_pubkey.as_ref(), &proof.signed_message()) {
        Ok(())
    } else {
        Err(IpProofError::InvalidSignature)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::SIGNED_MESSAGE_LEN;

    fn fixture() -> (Keypair, Pubkey, Ipv4Addr, u64, u8) {
        (
            Keypair::new(),
            Pubkey::new_unique(),
            Ipv4Addr::new(198, 51, 100, 42),
            931,
            3,
        )
    }

    #[test]
    fn sign_verify_round_trip() {
        let (verifier, payer, ip, epoch, user_type) = fixture();
        let proof = sign(&verifier, &payer, &ip, epoch, user_type);

        assert_eq!(proof.version, IP_PROOF_VERSION);
        assert_eq!(proof.payer, payer);
        assert_eq!(proof.client_ip, ip);
        assert_eq!(proof.epoch, epoch);
        assert_eq!(proof.user_type, user_type);
        assert_eq!(verify(&proof, &verifier.pubkey()), Ok(()));
    }

    #[test]
    fn verify_rejects_a_different_verifier_key() {
        let (verifier, payer, ip, epoch, user_type) = fixture();
        let proof = sign(&verifier, &payer, &ip, epoch, user_type);

        assert_eq!(
            verify(&proof, &Keypair::new().pubkey()),
            Err(IpProofError::InvalidSignature)
        );
    }

    #[test]
    fn verify_rejects_a_tampered_proof() {
        let (verifier, payer, ip, epoch, user_type) = fixture();
        let proof = sign(&verifier, &payer, &ip, epoch, user_type);

        for tampered in [
            IpOwnershipProof {
                payer: Pubkey::new_unique(),
                ..proof
            },
            IpOwnershipProof {
                client_ip: Ipv4Addr::new(198, 51, 100, 43),
                ..proof
            },
            IpOwnershipProof {
                epoch: epoch + 1,
                ..proof
            },
            IpOwnershipProof {
                user_type: user_type + 1,
                ..proof
            },
            IpOwnershipProof {
                version: proof.version + 1,
                ..proof
            },
        ] {
            assert_eq!(
                verify(&tampered, &verifier.pubkey()),
                Err(IpProofError::InvalidSignature)
            );
        }
    }

    #[test]
    fn sign_version_signs_the_requested_version() {
        let (verifier, payer, ip, epoch, user_type) = fixture();
        let proof = sign_version(2, &verifier, &payer, &ip, epoch, user_type);

        assert_eq!(proof.version, 2);
        assert_eq!(proof.signed_message()[11], 2);
        assert_eq!(verify(&proof, &verifier.pubkey()), Ok(()));
        assert!(!crate::is_supported_version(proof.version));
    }

    #[test]
    fn signature_covers_exactly_the_signed_message() {
        let (verifier, payer, ip, epoch, user_type) = fixture();
        let proof = sign(&verifier, &payer, &ip, epoch, user_type);

        let message = signed_message_for(IP_PROOF_VERSION, &payer, &ip, epoch, user_type);
        assert_eq!(message.len(), SIGNED_MESSAGE_LEN);
        assert!(Signature::from(proof.signature).verify(verifier.pubkey().as_ref(), &message));
    }
}
