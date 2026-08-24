use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_ip_proof::{is_supported_version, verify, IpOwnershipProof};
use doublezero_serviceability_instruction::ip_proof::with_ed25519_verification;
use solana_sdk::{instruction::Instruction, pubkey::Pubkey};
use std::net::Ipv4Addr;

pub mod check_access_pass;
pub mod create;
pub mod create_subscribe;
pub mod delete;
pub mod get;
pub mod list;
pub mod requestban;
pub mod update;

/// Pairs an RFC-27 user creation with the native `Ed25519SigVerify` instruction that proves its
/// `IpOwnershipProof`, resolving the verifier key from GlobalState.
///
/// Shared by `CreateUserCommand` and `CreateSubscribeUserCommand` for the same reason the program
/// validates both through one helper: the two must not drift.
///
/// `proof_owner` is the key the proof has to name. The program binds the proof to the *effective
/// owner* of the user being created, not the transaction payer, which differ on
/// `CreateSubscribeUser`'s foundation-allowlist `--owner` path.
///
/// The version and the three field comparisons mirror the program's own, and are made here rather
/// than left to it because onchain they surface as `IpProofVersionUnsupported`,
/// `IpProofPayerMismatch`, `IpProofClientIpMismatch` and `IpProofUserTypeMismatch` only after the
/// transaction has been paid for. The epoch window is deliberately not checked: the ledger's
/// current epoch is the program's to judge, and a proof this client thinks is stale may not be.
///
/// The signature check has no onchain counterpart to fall back on. The program compares the proof's
/// signature against the Ed25519 instruction's, but a signature that does not verify never reaches
/// the program: the precompile rejects the transaction in the leader, and with `skip_preflight`
/// the caller sees a confirmation timeout with no logs and no error. That is the one failure in
/// this path that is otherwise invisible, and it is reachable in normal operation — a client
/// holding a proof signed by a verifier key that has since rotated.
pub(crate) fn instructions_with_ip_proof(
    client: &dyn DoubleZeroClient,
    proof: &IpOwnershipProof,
    proof_owner: &Pubkey,
    client_ip: &Ipv4Addr,
    user_type: u8,
    create_instruction: Instruction,
) -> eyre::Result<Vec<Instruction>> {
    // The version travels in the proof so a v2 layout can roll out without an atomic cutover.
    // Checked first, as the program does: every comparison below is about a message this client
    // cannot even reconstruct for a version it does not know.
    if !is_supported_version(proof.version) {
        eyre::bail!(
            "IP ownership proof version {} is not supported by this client",
            proof.version
        );
    }
    if proof.payer != *proof_owner {
        eyre::bail!(
            "IP ownership proof was issued for {} but this user is created for {}",
            proof.payer,
            proof_owner
        );
    }
    if proof.client_ip != *client_ip {
        eyre::bail!(
            "IP ownership proof was issued for {} but this user is created for {client_ip}",
            proof.client_ip
        );
    }
    if proof.user_type != user_type {
        eyre::bail!(
            "IP ownership proof was issued for user type {} but this user is created as {user_type}",
            proof.user_type
        );
    }

    // The verifier key travels neither in the proof nor in the service's response: a client reads
    // it from GlobalState, the same place the program reads it.
    let (_, globalstate) = GetGlobalStateCommand.execute(client)?;
    let verifier = globalstate.ip_verifier_authority_pk;
    if verifier == Pubkey::default() {
        eyre::bail!(
            "No IP verifier authority is configured onchain; cannot attach an IP ownership proof"
        );
    }

    verify(proof, &verifier).map_err(|e| {
        eyre::eyre!(
            "IP ownership proof does not verify against the onchain verifier {verifier}: {e}"
        )
    })?;

    Ok(with_ed25519_verification(&verifier, proof, create_instruction).to_vec())
}
