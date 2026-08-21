//! RFC-27 IP ownership proof: the Ed25519 side of a user-creation transaction.
//!
//! A proof carried in `UserCreateArgs::ip_proof` / `UserCreateSubscribeArgs::ip_proof` is not
//! self-validating. BPF cannot verify Ed25519 cheaply, so the program checks the signature by
//! introspecting the Instructions sysvar for a native `Ed25519SigVerify` instruction that covers
//! the message the creation implies, signed by `globalstate.ip_verifier_authority_pk`
//! (`doublezero_serviceability::ip_proof`). A transaction that carries the proof but not that
//! instruction is rejected with `IpProofEd25519InstructionMissing`.
//!
//! The two builders here are the whole client-side contract: put the proof in the args (the
//! `create_user` / `create_subscribe_user` builders then append the Instructions sysvar account
//! themselves), and put [`ed25519_verification_instruction`] in the same transaction.
//! [`with_ed25519_verification`] does both halves for a caller that just wants a working
//! transaction.

use doublezero_ip_proof::IpOwnershipProof;
use solana_program::{instruction::Instruction, pubkey::Pubkey};

/// The native `Ed25519SigVerify` instruction that verifies `proof.signature` over
/// [`IpOwnershipProof::signed_message`] with `verifier`.
///
/// `verifier` is `globalstate.ip_verifier_authority_pk` — the proof does not carry the key it was
/// signed with, and the verification service deliberately does not return it, so a caller reads it
/// from GlobalState, the same place the program reads it. A mismatch is rejected onchain with
/// `IpProofVerifierKeyMismatch`.
///
/// The offset layout comes from `solana_ed25519_program`, the same code the runtime's precompile
/// parses, rather than being written out here: the program rejects any instruction whose offsets
/// name a different instruction or run past the end of its data, so a hand-rolled header is a
/// silent way to build a transaction that can never land.
pub fn ed25519_verification_instruction(
    verifier: &Pubkey,
    proof: &IpOwnershipProof,
) -> Instruction {
    solana_ed25519_program::new_ed25519_instruction_with_signature(
        &proof.signed_message(),
        &proof.signature,
        &verifier.to_bytes(),
    )
}

/// `[ed25519_verification_instruction(..), create_instruction]` — ready to send as one transaction
/// (after the caller's [`crate::compute_budget_prelude`]).
///
/// The program *scans* the Instructions sysvar rather than reading a fixed index, so it accepts the
/// Ed25519 instruction at any position and tolerates interleaved compute-budget instructions.
/// Ordering is therefore a convention, not a requirement — this helper pins it so a caller never
/// has to reason about it, and so the verification is visibly a precondition of the creation.
///
/// `create_instruction` must be a `create_user` / `create_subscribe_user` instruction whose args
/// carry the *same* proof: the args are what the program reconstructs the signed message from, and
/// those builders derive the Instructions sysvar account from `args.ip_proof.is_some()`.
pub fn with_ed25519_verification(
    verifier: &Pubkey,
    proof: &IpOwnershipProof,
    create_instruction: Instruction,
) -> [Instruction; 2] {
    [
        ed25519_verification_instruction(verifier, proof),
        create_instruction,
    ]
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_ip_proof::SIGNED_MESSAGE_LEN;
    use std::net::Ipv4Addr;

    // The precompile header the program re-derives in `doublezero_serviceability::ip_proof`:
    // `[num_signatures, padding, 7 x u16 offsets]`, then key, signature, message.
    const HEADER: usize = 16;
    const PUBLIC_KEY_OFFSET: usize = HEADER;
    const SIGNATURE_OFFSET: usize = PUBLIC_KEY_OFFSET + 32;
    const MESSAGE_OFFSET: usize = SIGNATURE_OFFSET + 64;

    fn proof() -> IpOwnershipProof {
        IpOwnershipProof {
            version: 1,
            payer: Pubkey::new_unique(),
            client_ip: Ipv4Addr::new(203, 0, 113, 7),
            epoch: 931,
            user_type: 3,
            signature: [7u8; 64],
        }
    }

    fn u16_at(data: &[u8], offset: usize) -> u16 {
        u16::from_le_bytes([data[offset], data[offset + 1]])
    }

    #[test]
    fn test_ed25519_verification_instruction_layout() {
        let verifier = Pubkey::new_unique();
        let proof = proof();
        let ix = ed25519_verification_instruction(&verifier, &proof);

        assert_eq!(ix.program_id, solana_program::ed25519_program::ID);
        // The precompile reads everything out of its own instruction data; it takes no accounts,
        // and an account here would silently change the transaction's key list.
        assert!(ix.accounts.is_empty());

        // Exactly one signature. The program rejects any other count with
        // `IpProofSignatureCountInvalid` — more than one would let an attacker pair the signature
        // the program checks with a second one it does not.
        assert_eq!(ix.data[0], 1);
        assert_eq!(ix.data[1], 0, "padding byte");

        // All three instruction indices must be the "this instruction" sentinel. An index naming
        // another instruction means the precompile verified bytes the program never reads, which
        // the program rejects with `IpProofEd25519OffsetsInvalid`.
        assert_eq!(u16_at(&ix.data, 2), SIGNATURE_OFFSET as u16);
        assert_eq!(u16_at(&ix.data, 4), u16::MAX, "signature_instruction_index");
        assert_eq!(u16_at(&ix.data, 6), PUBLIC_KEY_OFFSET as u16);
        assert_eq!(
            u16_at(&ix.data, 8),
            u16::MAX,
            "public_key_instruction_index"
        );
        assert_eq!(u16_at(&ix.data, 10), MESSAGE_OFFSET as u16);
        assert_eq!(u16_at(&ix.data, 12), SIGNED_MESSAGE_LEN as u16);
        assert_eq!(u16_at(&ix.data, 14), u16::MAX, "message_instruction_index");

        assert_eq!(
            &ix.data[PUBLIC_KEY_OFFSET..SIGNATURE_OFFSET],
            verifier.as_ref()
        );
        assert_eq!(&ix.data[SIGNATURE_OFFSET..MESSAGE_OFFSET], &proof.signature);
        assert_eq!(
            &ix.data[MESSAGE_OFFSET..MESSAGE_OFFSET + SIGNED_MESSAGE_LEN],
            proof.signed_message().as_slice()
        );
        assert_eq!(ix.data.len(), MESSAGE_OFFSET + SIGNED_MESSAGE_LEN);
    }

    #[test]
    fn test_ed25519_verification_instruction_covers_the_proofs_own_message() {
        // The signed bytes come from the proof, never from separately passed-in fields: a proof
        // whose message disagrees with what the program reconstructs is rejected onchain, and this
        // builder must not be the thing that introduces the disagreement.
        let verifier = Pubkey::new_unique();
        let mut proof = proof();
        let baseline = ed25519_verification_instruction(&verifier, &proof);

        proof.epoch += 1;
        let shifted = ed25519_verification_instruction(&verifier, &proof);

        assert_ne!(baseline.data, shifted.data);
        assert_eq!(
            &shifted.data[MESSAGE_OFFSET..MESSAGE_OFFSET + SIGNED_MESSAGE_LEN],
            proof.signed_message().as_slice()
        );
    }

    #[test]
    fn test_with_ed25519_verification_puts_the_ed25519_instruction_first() {
        let verifier = Pubkey::new_unique();
        let proof = proof();
        let create = Instruction::new_with_bytes(Pubkey::new_unique(), &[36], vec![]);

        let pair = with_ed25519_verification(&verifier, &proof, create.clone());

        assert_eq!(pair[0], ed25519_verification_instruction(&verifier, &proof));
        assert_eq!(pair[1], create);
    }
}
