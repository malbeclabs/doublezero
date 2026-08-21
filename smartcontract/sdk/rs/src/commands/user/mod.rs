use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_ip_proof::IpOwnershipProof;
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
/// The three field comparisons mirror the program's own, and are made here rather than left to it
/// because onchain they surface as `IpProofPayerMismatch`, `IpProofClientIpMismatch` and
/// `IpProofUserTypeMismatch` only after the transaction has been paid for. The epoch window is
/// deliberately not checked: the ledger's current epoch is the program's to judge, and a proof
/// this client thinks is stale may not be.
pub(crate) fn instructions_with_ip_proof(
    client: &dyn DoubleZeroClient,
    proof: &IpOwnershipProof,
    proof_owner: &Pubkey,
    client_ip: &Ipv4Addr,
    user_type: u8,
    create_instruction: Instruction,
) -> eyre::Result<Vec<Instruction>> {
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

    Ok(with_ed25519_verification(&verifier, proof, create_instruction).to_vec())
}
