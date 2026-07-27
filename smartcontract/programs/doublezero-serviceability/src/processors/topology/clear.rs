use crate::{
    authorize::{authorize, split_trailing_permission},
    error::DoubleZeroError,
    pda::{get_globalstate_pda, get_link_pda, get_topology_pda},
    processors::validation::validate_program_account,
    serializer::try_acc_write,
    state::{
        globalstate::GlobalState, link::Link, permission::permission_flags, topology::TopologyInfo,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    instruction::AccountMeta,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, Clone, PartialEq)]
pub struct TopologyClearArgs {
    pub name: String,
}

/// Canonical account list for one `ClearTopology` instruction, excluding the
/// payer/system_program/permission accounts the transaction builder appends.
///
/// The topology PDA is **writable**: `process_topology_clear` decrements its
/// `reference_count` for every link that actually drops a reference. The Rust SDK
/// builder and the program integration tests both build their account list here, so a
/// writability divergence between client and processor fails a test instead of a live
/// transaction.
///
/// This lives in the program crate rather than `doublezero-serviceability-instruction`
/// because topology builders are RFC-26 R7 and that crate's shape is
/// `build_xxx(..) -> Instruction`, not `-> Vec<AccountMeta>`. When R7 moves it, keep
/// the program integration tests building from whatever becomes canonical — a Cargo
/// dev-dependency cycle on the instruction crate is legal (dev-deps feed only test
/// targets) — or the anti-drift property this exists for is lost.
pub fn clear_topology_account_metas(
    program_id: &Pubkey,
    name: &str,
    link_pubkeys: &[Pubkey],
) -> Vec<AccountMeta> {
    let (topology_pda, _) = get_topology_pda(program_id, name);
    let (globalstate_pda, _) = get_globalstate_pda(program_id);
    let mut accounts = Vec::with_capacity(2 + link_pubkeys.len());
    accounts.push(AccountMeta::new(topology_pda, false));
    accounts.push(AccountMeta::new_readonly(globalstate_pda, false));
    accounts.extend(link_pubkeys.iter().map(|pk| AccountMeta::new(*pk, false)));
    accounts
}

/// Accounts layout:
/// [0]    topology PDA  (writable when account still exists; readonly is accepted when
///                       the topology has already been closed — clear is tolerant of that)
/// [1]    globalstate   (readonly)
/// [2..n] Link accounts (writable) — remove topology pubkey from link_topologies on each
/// [n+1]  payer         (writable, signer, must hold TOPOLOGY_ADMIN)
/// [n+2]  system_program
/// [n+3]  permission    (readonly, optional — payer's Permission PDA)
///
/// Note: payer and system_program are the last two accounts (or the last two
/// before the optional Permission account). The SDK client always appends them
/// after the variable-length link list.
pub fn process_topology_clear(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TopologyClearArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let topology_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_topology_clear(name={})", value.name);

    // The SDK client appends payer and system_program after the variable-length
    // Link list, plus an optional Permission account when one exists for the
    // payer. split_trailing_permission peels those off the tail.
    let all_remaining: Vec<&AccountInfo> = accounts_iter.collect();
    let (payer_account, _system_program, link_accounts, permission_account) =
        split_trailing_permission(program_id, &all_remaining)?;

    // Payer must be a signer
    if !payer_account.is_signer {
        msg!("TopologyClear: payer must be a signer");
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Validate GlobalState singleton PDA.
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        pda = &get_globalstate_pda(program_id).0,
        "GlobalState"
    );

    // Authorization: TOPOLOGY_ADMIN (Permission account) or foundation (legacy).
    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        &mut permission_account.into_iter(),
        payer_account.key,
        &globalstate,
        permission_flags::TOPOLOGY_ADMIN,
    )?;

    // Validate topology PDA. Clear is tolerant of an already-closed topology, so the
    // PDA match is the only unconditional check — validate_program_account! asserts
    // non-empty and would reject that case.
    let (expected_pda, _) = get_topology_pda(program_id, &value.name);
    assert_eq!(
        topology_account.key, &expected_pda,
        "TopologyClear: invalid topology PDA for name '{}'",
        value.name
    );
    // A topology that still carries data has its reference_count decremented below, so
    // it must be program-owned and writable. Checked here, before the link loop, so a
    // readonly topology account fails up front instead of part-way through the writes.
    if !topology_account.data_is_empty() {
        validate_program_account!(topology_account, program_id, writable = true, "Topology");
    }

    let topology_key = topology_account.key;
    let mut cleared_count: usize = 0;

    // Process remaining Link accounts: remove topology key from link_topologies
    for link_account in link_accounts.iter().copied() {
        validate_program_account!(link_account, program_id, writable = true, "Link");
        let mut link = Link::try_from(link_account)?;
        assert_eq!(
            link_account.key,
            &get_link_pda(program_id, link.index).0,
            "Invalid Link PDA"
        );
        let before_len = link.link_topologies.len();
        link.link_topologies.retain(|k| k != topology_key);
        if link.link_topologies.len() < before_len {
            try_acc_write(&link, link_account, payer_account, accounts)?;
            cleared_count += 1;
        }
    }

    // Decrement ref_count on the topology by the number of links that actually had
    // a reference removed. Skip when the topology is already closed — in that case
    // clear is purely a stale-reference cleanup on the link side.
    if !topology_account.data_is_empty() && cleared_count > 0 {
        let mut topology = TopologyInfo::try_from(topology_account)?;
        topology.reference_count = topology
            .reference_count
            .saturating_sub(cleared_count as u32);
        try_acc_write(&topology, topology_account, payer_account, accounts)?;
    }

    // Distinguish the stale-reference GC path: an already-closed (or never-created)
    // topology otherwise logs the same line as a live one, so a typo'd --name reads as
    // a successful clear.
    if topology_account.data_is_empty() {
        msg!(
            "TopologyClear: topology '{}' is already closed; cleaned stale references from {} link(s)",
            value.name,
            cleared_count
        );
    } else {
        msg!(
            "TopologyClear: removed topology '{}' from {} link(s)",
            value.name,
            cleared_count
        );
    }
    Ok(())
}
