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
/// [0] topology PDA     (writable when account still exists; readonly is accepted when
///                      the topology has already been closed — clear is tolerant of that)
/// [1] globalstate      (readonly)
/// [2] payer            (writable, signer, must hold TOPOLOGY_ADMIN)
/// [3] system_program
/// [4+] Link accounts   (writable) — remove topology pubkey from link_topologies on each
/// [last] permission    (readonly, optional — payer's Permission PDA, after the links)
pub fn process_topology_clear(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TopologyClearArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let topology_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_topology_clear(name={})", value.name);

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

    // The remaining accounts are the variable-length Link list, optionally
    // followed by the payer's Permission account (appended last by the SDK).
    // Peel it off so it is not mistaken for a Link in the loop below.
    let remaining: Vec<&AccountInfo> = accounts_iter.collect();
    let (permission_account, link_accounts) = match remaining.last() {
        Some(last) => {
            let (perm_pda, _) = get_permission_pda(program_id, payer_account.key);
            if last.key == &perm_pda {
                (Some(*last), &remaining[..remaining.len() - 1])
            } else {
                (None, &remaining[..])
            }
        }
        None => (None, &remaining[..]),
    };

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
