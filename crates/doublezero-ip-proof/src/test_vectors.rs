//! Committed cross-consumer test vectors, behind the `test-vectors` feature.
//!
//! The program tests, CLI tests, and verification-service tests all assert against these, so a
//! change to the signed-message layout that slips past one consumer's own tests still fails
//! everywhere else. Do not regenerate them to make a test pass: if a vector stops matching, the
//! wire format changed and [`crate::IP_PROOF_VERSION`] needs a bump.

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
    /// Base58.
    pub payer: &'static str,
    pub client_ip: &'static str,
    pub epoch: u64,
    /// Base58.
    pub user_pubkey: &'static str,
    /// Hex of the expected [`crate::signed_message_for`] output.
    pub signed_message_hex: &'static str,
    /// Hex of the expected signature by [`VERIFIER_PUBKEY`] over that message.
    pub signature_hex: &'static str,
}

impl TestVector {
    pub fn proof(&self) -> IpOwnershipProof {
        IpOwnershipProof {
            payer: self.payer(),
            client_ip: self.client_ip(),
            epoch: self.epoch,
            user_pubkey: self.user_pubkey(),
            signature: self.signature(),
        }
    }

    pub fn payer(&self) -> Pubkey {
        Pubkey::from_str(self.payer).expect("test vector payer is a valid pubkey")
    }

    pub fn client_ip(&self) -> Ipv4Addr {
        Ipv4Addr::from_str(self.client_ip).expect("test vector client_ip is a valid IPv4 address")
    }

    pub fn user_pubkey(&self) -> Pubkey {
        Pubkey::from_str(self.user_pubkey).expect("test vector user_pubkey is a valid pubkey")
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

/// A proof for a specific, ordinary client IP.
pub const SPECIFIC_IP: TestVector = TestVector {
    name: "specific client IP",
    payer: "29d2S7vB453rNYFdR5Ycwt7y9haRT5fwVwL9zTmBhfV2",
    client_ip: "203.0.113.7",
    epoch: 931,
    user_pubkey: "3JF3sEqM796hk5WFqA6EtmEwJQ9quALszsfJyvXNQKy3",
    signed_message_hex: "445a5f49505f50524f4f46011111111111111111111111111111111111111111111111111111111111111111cb007107a3030000000000002222222222222222222222222222222222222222222222222222222222222222",
    signature_hex: "f6339bb486ce1f0aff5f64f5a4b7dcc83a0ed331a0b407bded77243e0c242d15fb4b08a93dbe02c3757b7c768ee629caa2999df56be88379db346a436779380a",
};

/// A proof issued against a wildcard AccessPass, where the observed IP is the only per-IP control
/// and a high epoch exercises the upper bytes of the little-endian encoding.
pub const WILDCARD_PASS: TestVector = TestVector {
    name: "wildcard AccessPass connect",
    payer: "Bswb3UyeD1pUTaGiE6WvqwFpJZsQSEY1xhJePCDTHdvp",
    client_ip: "198.51.100.42",
    epoch: 72_057_594_037_927_936,
    user_pubkey: "D2ZcUbtpG5sKq7XLeB4YnpNnTGSptKCxTddoNeydzJQq",
    signed_message_hex: "445a5f49505f50524f4f4601a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1c633642a0000000000000001b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2b2",
    signature_hex: "b143de0201fabf97050055278656f7c4c9f9c7c84ba09f2401c2e1fd5b55405ca75ba079cc4d63708c3a9bf32524f8917a11e1cf2129dc7a887534fffd772801",
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
                    &vector.payer(),
                    &vector.client_ip(),
                    vector.epoch,
                    &vector.user_pubkey(),
                ),
                vector.signed_message(),
                "{}: signed message layout changed — bump IP_PROOF_VERSION rather than \
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
                crate::sign(
                    &keypair,
                    &vector.payer(),
                    &vector.client_ip(),
                    vector.epoch,
                    &vector.user_pubkey(),
                )
                .signature,
                vector.signature(),
                "{}: signing is expected to be deterministic",
                vector.name,
            );
        }
    }
}
