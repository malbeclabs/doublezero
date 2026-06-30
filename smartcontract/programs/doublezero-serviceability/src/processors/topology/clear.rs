use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::{get_globalstate_pda, get_link_pda, get_permission_pda, get_topology_pda},
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
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, Clone, PartialEq)]
pub struct TopologyClearArgs {
    pub name: String,
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

    // Collect remaining accounts. The SDK client always appends payer and
    // system_program at the end, after the variable-length Link list, plus an
    // optional Permission account when one exists for the payer.
    let all_remaining: Vec<&AccountInfo> = accounts_iter.collect();
    if all_remaining.len() < 2 {
        msg!("TopologyClear: expected at least payer and system_program accounts");
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    let n = all_remaining.len();
    // Detect an optional trailing Permission account. With it present the layout
    // is [links.., payer, system, permission]; the payer would then be at n-3,
    // so the last account is a Permission account iff it matches that payer's PDA.
    let permission_account = if n >= 3 {
        let candidate_payer = all_remaining[n - 3];
        let (perm_pda, _) = get_permission_pda(program_id, candidate_payer.key);
        (all_remaining[n - 1].key == &perm_pda).then_some(all_remaining[n - 1])
    } else {
        None
    };
    let (payer_account, _system_program, link_accounts) = if permission_account.is_some() {
        (
            all_remaining[n - 3],
            all_remaining[n - 2],
            &all_remaining[..n - 3],
        )
    } else {
        (
            all_remaining[n - 2],
            all_remaining[n - 1],
            &all_remaining[..n - 2],
        )
    };

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

    // Validate topology PDA. Clear is tolerant of an already-closed topology,
    // so we cannot call validate_program_account! (it asserts non-empty). If
    // the account does carry data, also verify it belongs to this program.
    let (expected_pda, _) = get_topology_pda(program_id, &value.name);
    assert_eq!(
        topology_account.key, &expected_pda,
        "TopologyClear: invalid topology PDA for name '{}'",
        value.name
    );
    if !topology_account.data_is_empty() {
        assert_eq!(
            topology_account.owner, program_id,
            "Invalid Topology Account Owner"
        );
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
        assert!(
            topology_account.is_writable,
            "Topology Account is not writable"
        );
        let mut topology = TopologyInfo::try_from(topology_account)?;
        topology.reference_count = topology
            .reference_count
            .saturating_sub(cleared_count as u32);
        try_acc_write(&topology, topology_account, payer_account, accounts)?;
    }

    msg!(
        "TopologyClear: removed topology '{}' from {} link(s)",
        value.name,
        cleared_count
    );
    Ok(())
}
