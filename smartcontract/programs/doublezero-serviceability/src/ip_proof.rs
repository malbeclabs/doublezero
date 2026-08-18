//! RFC-27 IP ownership proof validation.
//!
//! A `client_ip` is a plain instruction argument: nothing on-chain attests that the caller can
//! originate traffic from it. For a wildcard access pass — one stored at `0.0.0.0` or flagged
//! `allow_multiple_ip`, which includes the `EdgeSeat` passes the shred-oracle issues — the program
//! accepts any globally-routable address, so a registrant can squat the `(client_ip, user_type)`
//! User PDA of an address they do not control and point device tunnel provisioning at a third
//! party. RFC-27 closes that with a proof signed by a DoubleZero-operated verifier whose public key
//! lives in `GlobalState`.
//!
//! BPF cannot verify Ed25519 cheaply, so the signature is checked by the native Ed25519 precompile:
//! the client puts an `Ed25519SigVerify` instruction in the same transaction, and this module reads
//! the Instructions sysvar to confirm that instruction covers the message we reconstruct, with the
//! key we trust, carrying the signature the proof claims.

use crate::{
    error::DoubleZeroError,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
    },
};
use doublezero_ip_proof::{
    is_supported_version, signed_message_for, IpOwnershipProof, SIGNED_MESSAGE_LEN,
};
use solana_instructions_sysvar::load_instruction_at_checked;
use solana_program::{
    account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey,
    sysvar::instructions,
};
use std::net::Ipv4Addr;

// Ed25519 precompile layout (`solana_ed25519_program`), re-derived here so the program does not
// depend on a crate that pulls a userspace Ed25519 implementation into the BPF build.
//
//   [0]      num_signatures: u8
//   [1]      padding: u8
//   [2..16]  Ed25519SignatureOffsets — seven little-endian u16 fields
const NUM_SIGNATURES_OFFSET: usize = 0;
const SIGNATURE_OFFSETS_START: usize = 2;
const SIGNATURE_OFFSETS_SERIALIZED_SIZE: usize = 14;

/// Sentinel meaning "the data lives in this same instruction". Any other value names another
/// instruction in the transaction.
const CURRENT_INSTRUCTION: u16 = u16::MAX;

/// Splits a trailing Instructions sysvar account off the end of an account list.
///
/// The sysvar is appended **last**, after every other account both user-creation instructions
/// carry. Peeling it from the tail by its fixed key is a slice split with no allocation, and no
/// other account in either layout can ever hold that key — so it cannot be confused with
/// `CreateUser`'s length-detected `tenant` slot or `CreateSubscribeUser`'s PDA-matched trailing
/// `Permission`. Callers run their existing parsing over the shortened slice, unchanged.
pub fn split_trailing_instructions_sysvar<'a, 'info>(
    accounts: &'a [AccountInfo<'info>],
) -> (&'a [AccountInfo<'info>], Option<&'a AccountInfo<'info>>) {
    match accounts.split_last() {
        Some((last, rest)) if instructions::check_id(last.key) => (rest, Some(last)),
        _ => (accounts, None),
    }
}

/// Validates an RFC-27 IP ownership proof, or the absence of one.
///
/// With `FeatureFlag::RequireIpOwnershipProof` clear, a missing proof is accepted and a supplied
/// one is still validated in full — a client that attaches a bad proof is broken whether or not
/// enforcement has been switched on, and letting it through would mask that until the flag flips.
/// With the flag set, a proof is required for every user creation, wildcard and specific-IP passes
/// alike (#4192 item 3: a legacy path that skips the proof is exactly the hole the RFC exists to
/// close).
///
/// `user_type` binds the proof to the account it authorizes: the User PDA is
/// `f(client_ip, user_type)`, so `client_ip` alone would leave a proof reusable for a different
/// connection type on the same address within the same epoch.
pub fn validate_ip_ownership_proof(
    instructions_sysvar: Option<&AccountInfo>,
    proof: Option<&IpOwnershipProof>,
    globalstate: &GlobalState,
    payer: &Pubkey,
    client_ip: &Ipv4Addr,
    user_type: u8,
    current_epoch: u64,
) -> Result<(), ProgramError> {
    let proof = match proof {
        Some(proof) => proof,
        None => {
            return if is_feature_enabled(
                globalstate.feature_flags,
                FeatureFlag::RequireIpOwnershipProof,
            ) {
                msg!("IP ownership proof required but none supplied");
                Err(DoubleZeroError::IpOwnershipProofRequired.into())
            } else {
                Ok(())
            };
        }
    };

    // An unset verifier key is "no verifier configured", never "any signature passes". Checked
    // before anything else touches the key.
    let verifier = globalstate.ip_verifier_authority_pk;
    if verifier == Pubkey::default() {
        msg!("No ip_verifier_authority_pk configured; cannot validate an IP ownership proof");
        return Err(DoubleZeroError::IpVerifierNotConfigured.into());
    }

    // The version travels in the proof so a v2 layout can roll out without an atomic cutover, but
    // only versions this program can reconstruct are accepted: rebuilding v1 bytes for a proof
    // signed over some other layout would compare two unrelated messages.
    if !is_supported_version(proof.version) {
        msg!("Unsupported IP ownership proof version {}", proof.version);
        return Err(DoubleZeroError::IpProofVersionUnsupported.into());
    }

    // Field comparisons first: they are nearly free next to walking the sysvar, and each one names
    // its own failure.
    if proof.payer != *payer {
        msg!(
            "IP proof payer {} does not match transaction payer {}",
            proof.payer,
            payer
        );
        return Err(DoubleZeroError::IpProofPayerMismatch.into());
    }
    if proof.client_ip != *client_ip {
        msg!(
            "IP proof client_ip {} does not match requested client_ip {}",
            proof.client_ip,
            client_ip
        );
        return Err(DoubleZeroError::IpProofClientIpMismatch.into());
    }
    if proof.user_type != user_type {
        msg!(
            "IP proof user_type {} does not match requested user_type {}",
            proof.user_type,
            user_type
        );
        return Err(DoubleZeroError::IpProofUserTypeMismatch.into());
    }
    // The window is the current epoch and the one before it. A proof issued moments before an
    // epoch rollover must still work; anything older, or claiming an epoch that has not happened,
    // does not. `proof.epoch + 1 == current_epoch` also reads correctly at `current_epoch == 0`.
    if proof.epoch != current_epoch && proof.epoch.saturating_add(1) != current_epoch {
        msg!(
            "IP proof epoch {} outside the window [{}, {}]",
            proof.epoch,
            current_epoch.saturating_sub(1),
            current_epoch
        );
        return Err(DoubleZeroError::IpProofEpochOutOfWindow.into());
    }

    let sysvar = match instructions_sysvar {
        Some(account) if instructions::check_id(account.key) => account,
        _ => {
            msg!("Instructions sysvar account not supplied");
            return Err(DoubleZeroError::IpProofInstructionsSysvarMissing.into());
        }
    };

    let message = signed_message_for(proof.version, payer, client_ip, proof.epoch, user_type);
    find_matching_ed25519_instruction(sysvar, &verifier, &message, &proof.signature)
}

/// Scans the transaction for an Ed25519 precompile instruction that verifies exactly this
/// signature, over exactly this message, with exactly this key.
///
/// The scan is deliberate rather than a fixed relative index: the client is free to place the
/// Ed25519 instruction anywhere — before or after the program instruction, with compute-budget
/// instructions interleaved — and pinning a position would make the layout of an unrelated
/// instruction load-bearing.
fn find_matching_ed25519_instruction(
    sysvar: &AccountInfo,
    verifier: &Pubkey,
    message: &[u8; SIGNED_MESSAGE_LEN],
    signature: &[u8; 64],
) -> Result<(), ProgramError> {
    // The sysvar data opens with a little-endian u16 instruction count. Reading it directly is
    // cheaper than probing `load_instruction_at_checked` until it errors.
    let num_instructions = {
        let data = sysvar.try_borrow_data()?;
        if data.len() < 2 {
            msg!("Instructions sysvar data is truncated");
            return Err(DoubleZeroError::IpProofInstructionsSysvarMissing.into());
        }
        u16::from_le_bytes([data[0], data[1]])
    };

    // Remember why the first Ed25519 instruction was rejected. Returning that beats a generic
    // "none found" when the client attached one proof and got a detail wrong — which is the
    // common case, since a transaction carries at most one of these in practice.
    let mut first_rejection: Option<DoubleZeroError> = None;

    for index in 0..num_instructions {
        let instruction = load_instruction_at_checked(index as usize, sysvar)?;
        if !solana_program::ed25519_program::check_id(&instruction.program_id) {
            continue;
        }
        match check_ed25519_instruction(&instruction.data, index, verifier, message, signature) {
            Ok(()) => return Ok(()),
            Err(e) => first_rejection.get_or_insert(e),
        };
    }

    let error = first_rejection.unwrap_or(DoubleZeroError::IpProofEd25519InstructionMissing);
    msg!(
        "No Ed25519 instruction verifies the IP ownership proof: {}",
        error
    );
    Err(error.into())
}

/// Checks one Ed25519 precompile instruction against the proof.
///
/// `index` is that instruction's own position in the transaction, needed because the precompile's
/// offsets may name a *different* instruction to read the key, signature, or message from. If they
/// do, the precompile verified bytes this function never sees, and comparing against this
/// instruction's own data would accept a signature over something else entirely — so any offset
/// that points elsewhere is rejected outright.
fn check_ed25519_instruction(
    data: &[u8],
    index: u16,
    verifier: &Pubkey,
    message: &[u8; SIGNED_MESSAGE_LEN],
    signature: &[u8; 64],
) -> Result<(), DoubleZeroError> {
    if data.len() < SIGNATURE_OFFSETS_START + SIGNATURE_OFFSETS_SERIALIZED_SIZE {
        return Err(DoubleZeroError::IpProofEd25519OffsetsInvalid);
    }
    // Exactly one signature. Zero means nothing was verified at all; with several, "which one is
    // the proof's" has no answer the program can reach without trusting the caller's ordering.
    if data[NUM_SIGNATURES_OFFSET] != 1 {
        return Err(DoubleZeroError::IpProofSignatureCountInvalid);
    }

    let field = |i: usize| -> u16 {
        let at = SIGNATURE_OFFSETS_START + i * 2;
        u16::from_le_bytes([data[at], data[at + 1]])
    };
    let signature_offset = field(0);
    let signature_instruction_index = field(1);
    let public_key_offset = field(2);
    let public_key_instruction_index = field(3);
    let message_data_offset = field(4);
    let message_data_size = field(5);
    let message_instruction_index = field(6);

    let is_self = |i: u16| i == CURRENT_INSTRUCTION || i == index;
    if !is_self(signature_instruction_index)
        || !is_self(public_key_instruction_index)
        || !is_self(message_instruction_index)
    {
        return Err(DoubleZeroError::IpProofEd25519OffsetsInvalid);
    }

    let slice = |offset: u16, len: usize| -> Option<&[u8]> {
        let start = offset as usize;
        data.get(start..start.checked_add(len)?)
    };

    let Some(public_key) = slice(public_key_offset, 32) else {
        return Err(DoubleZeroError::IpProofEd25519OffsetsInvalid);
    };
    let Some(instruction_signature) = slice(signature_offset, 64) else {
        return Err(DoubleZeroError::IpProofEd25519OffsetsInvalid);
    };
    let Some(instruction_message) = slice(message_data_offset, message_data_size as usize) else {
        return Err(DoubleZeroError::IpProofEd25519OffsetsInvalid);
    };

    if public_key != verifier.as_ref() {
        return Err(DoubleZeroError::IpProofVerifierKeyMismatch);
    }
    if instruction_signature != signature {
        return Err(DoubleZeroError::IpProofSignatureMismatch);
    }
    if instruction_message != message {
        return Err(DoubleZeroError::IpProofMessageMismatch);
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_ip_proof::SIGNED_MESSAGE_LEN;

    // The runtime's Ed25519 precompile validates its own layout, so several of the shapes below
    // never reach the program through a real transaction (the integration tests in
    // tests/user_ip_proof_test.rs assert exactly that). These unit tests cover the program's own
    // checks directly, because the program must not depend on that outer layer staying strict —
    // and because a precompile that verified bytes the program never reads is the one failure
    // mode that turns proof validation into theatre.

    const HEADER: u16 = 16;

    fn message() -> [u8; SIGNED_MESSAGE_LEN] {
        [7u8; SIGNED_MESSAGE_LEN]
    }

    fn signature() -> [u8; 64] {
        [9u8; 64]
    }

    fn verifier() -> Pubkey {
        Pubkey::new_from_array([3u8; 32])
    }

    /// A precompile instruction laid out the way a well-behaved client emits one.
    fn instruction_data(
        num_signatures: u8,
        signature_instruction_index: u16,
        public_key_instruction_index: u16,
        message_instruction_index: u16,
    ) -> Vec<u8> {
        let public_key_offset = HEADER;
        let signature_offset = public_key_offset + 32;
        let message_data_offset = signature_offset + 64;

        let mut data = Vec::new();
        data.push(num_signatures);
        data.push(0);
        for field in [
            signature_offset,
            signature_instruction_index,
            public_key_offset,
            public_key_instruction_index,
            message_data_offset,
            SIGNED_MESSAGE_LEN as u16,
            message_instruction_index,
        ] {
            data.extend_from_slice(&field.to_le_bytes());
        }
        data.extend_from_slice(verifier().as_ref());
        data.extend_from_slice(&signature());
        data.extend_from_slice(&message());
        data
    }

    fn check(data: &[u8], index: u16) -> Result<(), DoubleZeroError> {
        check_ed25519_instruction(data, index, &verifier(), &message(), &signature())
    }

    #[test]
    fn accepts_a_well_formed_instruction() {
        let data = instruction_data(1, u16::MAX, u16::MAX, u16::MAX);
        assert_eq!(check(&data, 0), Ok(()));
        // The sentinel is index-independent, so the same bytes work wherever the instruction sits.
        assert_eq!(check(&data, 5), Ok(()));
    }

    #[test]
    fn accepts_offsets_naming_this_instruction_explicitly() {
        // A client may write its own index instead of the u16::MAX sentinel; both mean "here".
        let data = instruction_data(1, 3, 3, 3);
        assert_eq!(check(&data, 3), Ok(()));
        // ...and the same bytes at a different position now name somebody else.
        assert_eq!(
            check(&data, 4),
            Err(DoubleZeroError::IpProofEd25519OffsetsInvalid)
        );
    }

    #[test]
    fn rejects_offsets_naming_another_instruction() {
        // The load-bearing case. Each of the three indices independently redirects what the
        // precompile reads; if any is allowed to point elsewhere, the bytes this function compares
        // are not the bytes that were verified.
        for (sig, key, msg) in [
            (0, u16::MAX, u16::MAX),
            (u16::MAX, 0, u16::MAX),
            (u16::MAX, u16::MAX, 0),
        ] {
            let data = instruction_data(1, sig, key, msg);
            assert_eq!(
                check(&data, 1),
                Err(DoubleZeroError::IpProofEd25519OffsetsInvalid),
                "offsets ({sig}, {key}, {msg}) must be rejected"
            );
        }
    }

    #[test]
    fn rejects_more_than_one_signature() {
        let data = instruction_data(2, u16::MAX, u16::MAX, u16::MAX);
        assert_eq!(
            check(&data, 0),
            Err(DoubleZeroError::IpProofSignatureCountInvalid)
        );
    }

    #[test]
    fn rejects_zero_signatures() {
        // Nothing was verified at all, however well-formed the rest of the layout is.
        let data = instruction_data(0, u16::MAX, u16::MAX, u16::MAX);
        assert_eq!(
            check(&data, 0),
            Err(DoubleZeroError::IpProofSignatureCountInvalid)
        );
    }

    #[test]
    fn rejects_data_too_short_for_the_offsets() {
        for len in 0..(SIGNATURE_OFFSETS_START + SIGNATURE_OFFSETS_SERIALIZED_SIZE) {
            let data = vec![1u8; len];
            assert_eq!(
                check(&data, 0),
                Err(DoubleZeroError::IpProofEd25519OffsetsInvalid),
                "{len} bytes must be rejected without indexing past the end"
            );
        }
    }

    #[test]
    fn rejects_offsets_running_past_the_end() {
        // Each field is pushed out of range in turn. The point is as much that nothing panics as
        // that the result is an error: an unchecked add here would wrap and read arbitrary bytes.
        // Byte offsets of signature_offset, public_key_offset, message_data_offset within the
        // offsets block. The odd-numbered slots between them are instruction indices, for which
        // u16::MAX is the legitimate "this instruction" sentinel rather than an out-of-range value.
        for offset_field in [0usize, 4, 8] {
            let mut data = instruction_data(1, u16::MAX, u16::MAX, u16::MAX);
            let at = SIGNATURE_OFFSETS_START + offset_field;
            data[at..at + 2].copy_from_slice(&u16::MAX.to_le_bytes());
            assert_eq!(
                check(&data, 0),
                Err(DoubleZeroError::IpProofEd25519OffsetsInvalid)
            );
        }

        // A length that overflows when added to a valid offset.
        let mut data = instruction_data(1, u16::MAX, u16::MAX, u16::MAX);
        let at = SIGNATURE_OFFSETS_START + 10;
        data[at..at + 2].copy_from_slice(&u16::MAX.to_le_bytes());
        assert_eq!(
            check(&data, 0),
            Err(DoubleZeroError::IpProofEd25519OffsetsInvalid)
        );
    }

    #[test]
    fn rejects_a_different_verifier_key() {
        let mut data = instruction_data(1, u16::MAX, u16::MAX, u16::MAX);
        data[HEADER as usize] ^= 0xff;
        assert_eq!(
            check(&data, 0),
            Err(DoubleZeroError::IpProofVerifierKeyMismatch)
        );
    }

    #[test]
    fn rejects_a_different_signature() {
        let mut data = instruction_data(1, u16::MAX, u16::MAX, u16::MAX);
        data[HEADER as usize + 32] ^= 0xff;
        assert_eq!(
            check(&data, 0),
            Err(DoubleZeroError::IpProofSignatureMismatch)
        );
    }

    #[test]
    fn rejects_a_different_message() {
        let mut data = instruction_data(1, u16::MAX, u16::MAX, u16::MAX);
        data[HEADER as usize + 32 + 64] ^= 0xff;
        assert_eq!(
            check(&data, 0),
            Err(DoubleZeroError::IpProofMessageMismatch)
        );
    }

    #[test]
    fn rejects_a_message_of_the_wrong_length() {
        // A shorter message that is a prefix of ours would otherwise compare equal on the bytes it
        // covers; the length is part of what was signed.
        let mut data = instruction_data(1, u16::MAX, u16::MAX, u16::MAX);
        let at = SIGNATURE_OFFSETS_START + 10;
        data[at..at + 2].copy_from_slice(&((SIGNED_MESSAGE_LEN - 1) as u16).to_le_bytes());
        assert_eq!(
            check(&data, 0),
            Err(DoubleZeroError::IpProofMessageMismatch)
        );
    }
}
