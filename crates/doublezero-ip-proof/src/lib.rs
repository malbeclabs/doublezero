//! The RFC-27 `IpOwnershipProof`: a signed attestation from the DoubleZero IP verification
//! service that a given payer originated a request from a given client IP around a given epoch,
//! for a given user type.
//!
//! The signed bytes are consumed by three independent places — the serviceability program (BPF),
//! the CLI, and the verification service — which must agree byte for byte or every proof fails
//! validation. That layout is defined here once, in [`signed_message_for`].
//!
//! The default build is BPF-clean. Signing and verification live behind the `signer` feature so
//! no crypto crate reaches the program, which verifies through the Ed25519 precompile instead.

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;
use std::net::Ipv4Addr;

#[cfg(feature = "signer")]
mod signer;
#[cfg(feature = "signer")]
pub use signer::{sign, sign_version, verify};

#[cfg(feature = "test-vectors")]
pub mod test_vectors;

/// Domain-separation prefix. Keeps the signed bytes from colliding with any other DoubleZero
/// message a verifier key might ever be asked to sign.
pub const IP_PROOF_DOMAIN: &[u8; 11] = b"DZ_IP_PROOF";

/// Layout version an issuer writes into [`IpOwnershipProof::version`]. Bump for any change to the
/// field set or ordering — for example a v2 that carries an IPv6 client address.
pub const IP_PROOF_VERSION: u8 = 1;

/// Versions a verifier accepts. The version travels in the proof, so a new layout can be rolled
/// out by teaching verifiers to accept both before issuers start writing the new one; drop the old
/// entry once no outstanding proof can still be inside its freshness window.
pub const SUPPORTED_IP_PROOF_VERSIONS: &[u8] = &[IP_PROOF_VERSION];

/// Whether a verifier accepts proofs at this layout version.
pub fn is_supported_version(version: u8) -> bool {
    SUPPORTED_IP_PROOF_VERSIONS.contains(&version)
}

const DOMAIN_END: usize = IP_PROOF_DOMAIN.len();
const VERSION_END: usize = DOMAIN_END + 1;
const PAYER_END: usize = VERSION_END + 32;
const CLIENT_IP_END: usize = PAYER_END + 4;
const EPOCH_END: usize = CLIENT_IP_END + 8;
const USER_TYPE_END: usize = EPOCH_END + 1;

/// Length of the signed message: prefix(11) + version(1) + payer(32) + client_ip(4) + epoch(8) +
/// user_type(1). Derived from the field offsets so inserting a field cannot leave the length
/// behind.
pub const SIGNED_MESSAGE_LEN: usize = USER_TYPE_END;

/// A signed attestation of IP ownership, carried in instruction data alongside the Ed25519
/// precompile instruction that actually verifies [`IpOwnershipProof::signature`].
#[derive(BorshSerialize, BorshDeserialize, Debug, Clone, Copy, PartialEq, Eq)]
pub struct IpOwnershipProof {
    /// Layout version of the signed message, written by the issuer. Verifiers reconstruct the
    /// bytes for *this* version and reject a version they do not support.
    pub version: u8,
    /// The account paying for / owning the user being created.
    pub payer: Pubkey,
    /// The address the verification service observed the request originate from.
    pub client_ip: Ipv4Addr,
    /// The DoubleZero ledger epoch the proof was issued in.
    pub epoch: u64,
    /// Discriminant of the serviceability `UserType` this proof authorizes, kept as a `u8` so this
    /// crate does not depend on the program it is consumed by. Binding it stops a proof obtained
    /// for one connection type from being replayed into a different one on the same IP in the same
    /// epoch — the User PDA is `f(client_ip, user_type)`, so `client_ip` alone does not pin the
    /// account being created.
    pub user_type: u8,
    /// Ed25519 signature by the verifier key over [`IpOwnershipProof::signed_message`].
    pub signature: [u8; 64],
}

impl IpOwnershipProof {
    /// The exact bytes the verifier signed. See [`signed_message_for`].
    pub fn signed_message(&self) -> [u8; SIGNED_MESSAGE_LEN] {
        signed_message_for(
            self.version,
            &self.payer,
            &self.client_ip,
            self.epoch,
            self.user_type,
        )
    }
}

/// Builds the signed message from its parts, without needing a proof in hand — the program and
/// the service both reconstruct it from instruction arguments.
///
/// Fixed layout, no length prefixes and no Borsh: every field has a known size, so the bytes are
/// unambiguous and the program can build them on the stack.
///
/// ```text
/// offset  len  field
///      0   11  b"DZ_IP_PROOF"
///     11    1  version
///     12   32  payer
///     44    4  client_ip, network order
///     48    8  epoch, little-endian
///     56    1  user_type
/// ```
pub fn signed_message_for(
    version: u8,
    payer: &Pubkey,
    client_ip: &Ipv4Addr,
    epoch: u64,
    user_type: u8,
) -> [u8; SIGNED_MESSAGE_LEN] {
    let mut message = [0u8; SIGNED_MESSAGE_LEN];
    message[..DOMAIN_END].copy_from_slice(IP_PROOF_DOMAIN);
    message[DOMAIN_END] = version;
    message[VERSION_END..PAYER_END].copy_from_slice(payer.as_ref());
    message[PAYER_END..CLIENT_IP_END].copy_from_slice(&client_ip.octets());
    message[CLIENT_IP_END..EPOCH_END].copy_from_slice(&epoch.to_le_bytes());
    message[EPOCH_END] = user_type;
    message
}

#[cfg(feature = "signer")]
#[derive(thiserror::Error, Debug, PartialEq, Eq)]
pub enum IpProofError {
    #[error("ip ownership proof signature does not verify against the verifier key")]
    InvalidSignature,
}

#[cfg(test)]
mod tests {
    use super::*;
    use borsh::to_vec;

    // Fixed inputs, so the expected message below is a stable vector rather than something
    // derived by the same code it is meant to check.
    fn proof() -> IpOwnershipProof {
        IpOwnershipProof {
            version: 1,
            payer: Pubkey::new_from_array([1u8; 32]),
            client_ip: Ipv4Addr::new(203, 0, 113, 7),
            epoch: 0x0102_0304_0506_0708,
            user_type: 3,
            signature: [3u8; 64],
        }
    }

    #[test]
    fn signed_message_matches_expected_bytes() {
        let mut expected = Vec::with_capacity(SIGNED_MESSAGE_LEN);
        expected.extend_from_slice(b"DZ_IP_PROOF");
        expected.push(1);
        expected.extend_from_slice(&[1u8; 32]);
        expected.extend_from_slice(&[203, 0, 113, 7]);
        expected.extend_from_slice(&[0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01]);
        expected.push(3);

        assert_eq!(proof().signed_message().as_slice(), expected.as_slice());
    }

    #[test]
    fn signed_message_layout_is_pinned() {
        assert_eq!(SIGNED_MESSAGE_LEN, 57);

        let message = proof().signed_message();
        assert_eq!(&message[..11], b"DZ_IP_PROOF");
        assert_eq!(message[11], IP_PROOF_VERSION);
        assert_eq!(&message[12..44], &[1u8; 32]);
        assert_eq!(&message[44..48], &[203, 0, 113, 7]);
        assert_eq!(message[48], 0x08, "epoch is little-endian");
        assert_eq!(message[56], 3);
    }

    #[test]
    fn signed_message_is_sensitive_to_every_field() {
        let base = proof().signed_message();

        let mut other = proof();
        other.version = 2;
        assert_ne!(base, other.signed_message());

        let mut other = proof();
        other.payer = Pubkey::new_from_array([9u8; 32]);
        assert_ne!(base, other.signed_message());

        let mut other = proof();
        other.client_ip = Ipv4Addr::new(203, 0, 113, 8);
        assert_ne!(base, other.signed_message());

        let mut other = proof();
        other.epoch += 1;
        assert_ne!(base, other.signed_message());

        let mut other = proof();
        other.user_type = 0;
        assert_ne!(base, other.signed_message());

        // The signature is not part of what is signed.
        let mut other = proof();
        other.signature = [4u8; 64];
        assert_eq!(base, other.signed_message());
    }

    #[test]
    fn a_proof_reconstructs_the_version_it_carries_not_the_current_one() {
        let mut v2 = proof();
        v2.version = 2;

        assert_eq!(v2.signed_message()[11], 2);
        assert_eq!(
            v2.signed_message(),
            signed_message_for(2, &v2.payer, &v2.client_ip, v2.epoch, v2.user_type),
        );
    }

    #[test]
    fn only_the_declared_versions_are_supported() {
        assert!(is_supported_version(IP_PROOF_VERSION));
        assert!(!is_supported_version(0));
        assert!(!is_supported_version(2));
    }

    #[test]
    fn borsh_round_trip() {
        let original = proof();
        let bytes = to_vec(&original).unwrap();

        // 1 + 32 + 4 + 8 + 1 + 64. Pinned so a field reorder or width change fails loudly.
        assert_eq!(bytes.len(), 110);

        assert_eq!(IpOwnershipProof::try_from_slice(&bytes).unwrap(), original);
    }

    #[test]
    fn borsh_serializes_client_ip_in_network_order() {
        let bytes = to_vec(&proof()).unwrap();
        assert_eq!(&bytes[33..37], &[203, 0, 113, 7]);
    }
}
