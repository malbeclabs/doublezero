//! Committed cross-consumer test vectors, behind the `test-vectors` feature.
//!
//! The program tests, CLI tests, and verification-service tests all assert against these, so a
//! change to the signed-message layout that slips past one consumer's own tests still fails
//! everywhere else. Do not regenerate them to make a test pass: if a vector stops matching, the
//! wire format changed and it needs a new [`crate::IP_PROOF_VERSION`] alongside the old one in
//! [`crate::SUPPORTED_IP_PROOF_VERSIONS`], not an edited vector.

use crate::{IpOwnershipProof, SIGNED_MESSAGE_LEN};
use solana_program::pubkey::Pubkey;
use std::{net::Ipv4Addr, str::FromStr};

/// Ed25519 keypair (32-byte seed followed by the 32-byte public key) that signed every vector
/// below. Test-only; it has no role outside this module.
pub const VERIFIER_KEYPAIR_HEX: &str = "0707070707070707070707070707070707070707070707070707070707070707ea4a6c63e29c520abef5507b132ec5f9954776aebebe7b92421eea691446d22c";

/// Base58 public key of [`VERIFIER_KEYPAIR_HEX`].
pub const VERIFIER_PUBKEY: &str = "GmaDrppBC7P5ARKV8g3djiwP89vz1jLK23V2GBjuAEGB";

pub struct TestVector {
    /// What this vector stands for, for assertion failure messages.
    pub name: &'static str,
    /// Layout version the vector was issued at.
    pub version: u8,
    /// Base58.
    pub payer: &'static str,
    pub client_ip: &'static str,
    pub epoch: u64,
    /// Serviceability `UserType` discriminant.
    pub user_type: u8,
    /// Hex of the expected [`crate::signed_message_for`] output.
    pub signed_message_hex: &'static str,
    /// Hex of the expected signature by [`VERIFIER_PUBKEY`] over that message.
    pub signature_hex: &'static str,
}

impl TestVector {
    pub fn proof(&self) -> IpOwnershipProof {
        IpOwnershipProof {
            version: self.version,
            payer: self.payer(),
            client_ip: self.client_ip(),
            epoch: self.epoch,
            user_type: self.user_type,
            signature: self.signature(),
        }
    }

    pub fn payer(&self) -> Pubkey {
        Pubkey::from_str(self.payer).expect("test vector payer is a valid pubkey")
    }

    pub fn client_ip(&self) -> Ipv4Addr {
        Ipv4Addr::from_str(self.client_ip).expect("test vector client_ip is a valid IPv4 address")
    }

    pub fn signed_message(&self) -> [u8; SIGNED_MESSAGE_LEN] {
        decode_array(self.signed_message_hex)
    }

    pub fn signature(&self) -> [u8; 64] {
        decode_array(self.signature_hex)
    }
}

/// The public key the vectors' signatures verify against.
pub fn verifier_pubkey() -> Pubkey {
    Pubkey::from_str(VERIFIER_PUBKEY).expect("test vector verifier pubkey is valid")
}

/// A proof for a specific, ordinary client IP, at the default `UserType` discriminant.
pub const SPECIFIC_IP: TestVector = TestVector {
    name: "specific client IP",
    version: 1,
    payer: "29d2S7vB453rNYFdR5Ycwt7y9haRT5fwVwL9zTmBhfV2",
    client_ip: "203.0.113.7",
    epoch: 931,
    user_type: 0,
    signed_message_hex: "445a5f49505f50524f4f46011111111111111111111111111111111111111111111111111111111111111111cb007107a30300000000000000",
    signature_hex: "30890c00d8847a9be171d7c087eef6bd92c62034eb31e568cab1e2465e0bf500ef9540e42b1d0851fcd6668aad4c3b672bf7459c2f1455be8248c8abd8739306",
};

/// A proof issued against a wildcard AccessPass, where the observed IP is the only per-IP control.
/// A high epoch exercises the upper bytes of the little-endian encoding, and a non-zero
/// `user_type` catches a dropped trailing byte.
pub const WILDCARD_PASS: TestVector = TestVector {
    name: "wildcard AccessPass connect",
    version: 1,
    payer: "Bswb3UyeD1pUTaGiE6WvqwFpJZsQSEY1xhJePCDTHdvp",
    client_ip: "198.51.100.42",
    epoch: 72_057_594_037_927_936,
    user_type: 3,
    signed_message_hex: "445a5f49505f50524f4f4601a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1c633642a000000000000000103",
    signature_hex: "205d6b00f2d9ee9ac1ed37e7cc324bcc945f3b351e63d84ec73e272754d43359daec40ee8f1b97eabd711b6cdc971cebd20bf93f9f32e72aee297a5d43f9be00",
};

pub const ALL: &[TestVector] = &[SPECIFIC_IP, WILDCARD_PASS];

fn decode_array<const N: usize>(value: &str) -> [u8; N] {
    let bytes = hex::decode(value).expect("test vector is valid hex");
    bytes
        .try_into()
        .unwrap_or_else(|_| panic!("test vector should decode to {N} bytes"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::signed_message_for;

    #[test]
    fn every_vector_rederives_its_signed_message() {
        for vector in ALL {
            assert_eq!(
                signed_message_for(
                    vector.version,
                    &vector.payer(),
                    &vector.client_ip(),
                    vector.epoch,
                    vector.user_type,
                ),
                vector.signed_message(),
                "{}: signed message layout changed — add a new IP_PROOF_VERSION rather than \
                 regenerating this vector",
                vector.name,
            );
            assert_eq!(vector.proof().signed_message(), vector.signed_message());
        }
    }

    #[cfg(feature = "signer")]
    #[test]
    fn every_vector_verifies_against_the_committed_verifier_key() {
        use solana_keypair::Keypair;
        use solana_signer::Signer;

        let keypair = Keypair::try_from(
            hex::decode(VERIFIER_KEYPAIR_HEX)
                .expect("keypair hex is valid")
                .as_slice(),
        )
        .expect("keypair bytes are a valid ed25519 keypair");
        assert_eq!(keypair.pubkey(), verifier_pubkey());

        for vector in ALL {
            assert_eq!(
                crate::verify(&vector.proof(), &verifier_pubkey()),
                Ok(()),
                "{}",
                vector.name,
            );
            assert_eq!(
                crate::sign_version(
                    vector.version,
                    &keypair,
                    &vector.payer(),
                    &vector.client_ip(),
                    vector.epoch,
                    vector.user_type,
                )
                .signature,
                vector.signature(),
                "{}: signing is expected to be deterministic",
                vector.name,
            );
        }
    }
}
