//! Off-chain signing and verification, behind the `signer` feature.
//!
//! The serviceability program does not use this module: onchain, the signature is checked by the
//! native Ed25519 precompile and the program only reconstructs [`signed_message_for`] and
//! compares it against what that instruction covers.

use crate::{signed_message_for, IpOwnershipProof, IpProofError};
use solana_keypair::Keypair;
use solana_program::pubkey::Pubkey;
use solana_signature::Signature;
use solana_signer::Signer;
use std::net::Ipv4Addr;

/// Issues a proof signed by the verifier keypair.
pub fn sign(
    verifier: &Keypair,
    payer: &Pubkey,
    client_ip: &Ipv4Addr,
    epoch: u64,
    user_pubkey: &Pubkey,
) -> IpOwnershipProof {
    let signature =
        verifier.sign_message(&signed_message_for(payer, client_ip, epoch, user_pubkey));

    IpOwnershipProof {
        payer: *payer,
        client_ip: *client_ip,
        epoch,
        user_pubkey: *user_pubkey,
        signature: signature.into(),
    }
}

/// Checks the proof's signature against the verifier public key.
///
/// This says nothing about freshness or about whether the proof matches the operation it is being
/// presented for — the caller still checks the epoch window, the payer, the client IP, and the
/// user account.
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

    fn fixture() -> (Keypair, Pubkey, Ipv4Addr, u64, Pubkey) {
        (
            Keypair::new(),
            Pubkey::new_unique(),
            Ipv4Addr::new(198, 51, 100, 42),
            931,
            Pubkey::new_unique(),
        )
    }

    #[test]
    fn sign_verify_round_trip() {
        let (verifier, payer, ip, epoch, user) = fixture();
        let proof = sign(&verifier, &payer, &ip, epoch, &user);

        assert_eq!(proof.payer, payer);
        assert_eq!(proof.client_ip, ip);
        assert_eq!(proof.epoch, epoch);
        assert_eq!(proof.user_pubkey, user);
        assert_eq!(verify(&proof, &verifier.pubkey()), Ok(()));
    }

    #[test]
    fn verify_rejects_a_different_verifier_key() {
        let (verifier, payer, ip, epoch, user) = fixture();
        let proof = sign(&verifier, &payer, &ip, epoch, &user);

        assert_eq!(
            verify(&proof, &Keypair::new().pubkey()),
            Err(IpProofError::InvalidSignature)
        );
    }

    #[test]
    fn verify_rejects_a_tampered_proof() {
        let (verifier, payer, ip, epoch, user) = fixture();
        let proof = sign(&verifier, &payer, &ip, epoch, &user);

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
                user_pubkey: Pubkey::new_unique(),
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
    fn signature_covers_exactly_the_signed_message() {
        let (verifier, payer, ip, epoch, user) = fixture();
        let proof = sign(&verifier, &payer, &ip, epoch, &user);

        let message = signed_message_for(&payer, &ip, epoch, &user);
        assert_eq!(message.len(), SIGNED_MESSAGE_LEN);
        assert!(Signature::from(proof.signature).verify(verifier.pubkey().as_ref(), &message));
    }
}
