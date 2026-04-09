use crate::{
    error::DoubleZeroError,
    pda::{get_resource_extension_pda, get_topology_pda},
    processors::resource::{allocate_id, allocate_specific_id},
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        device::Device,
        globalstate::GlobalState,
        interface::{Interface, LoopbackType},
        topology::FlexAlgoNodeSegment,
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
pub struct TopologyBackfillArgs {
    pub name: String,
}

/// Backfill FlexAlgoNodeSegment entries on existing Vpnv4 loopbacks for an
/// already-created topology. Idempotent — skips loopbacks that already have
/// an entry for this topology.
///
/// Accounts layout:
/// [0]  topology PDA        (readonly — must already exist)
/// [1]  segment_routing_ids (writable, ResourceExtension)
/// [2]  globalstate         (readonly)
/// [3]  payer               (writable, signer, must be in foundation_allowlist)
/// [4+] Device accounts     (writable)
pub fn process_topology_backfill(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TopologyBackfillArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let topology_account = next_account_info(accounts_iter)?;
    let segment_routing_ids_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_topology_backfill(name={})", value.name);

    if !payer_account.is_signer {
        msg!("TopologyBackfill: payer must be a signer");
        return Err(DoubleZeroError::Unauthorized.into());
    }

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        msg!("TopologyBackfill: unauthorized — foundation key required");
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Validate topology PDA
    let (expected_pda, _) = get_topology_pda(program_id, &value.name);
    if topology_account.key != &expected_pda {
        msg!(
            "TopologyBackfill: invalid topology PDA for name '{}'",
            value.name
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    if topology_account.data_is_empty() {
        msg!("TopologyBackfill: topology '{}' does not exist", value.name);
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    // Validate SegmentRoutingIds account
    let (expected_sr_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
    if segment_routing_ids_account.key != &expected_sr_pda {
        msg!("TopologyBackfill: invalid SegmentRoutingIds PDA");
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let topology_key = topology_account.key;
    let mut backfilled_count: usize = 0;
    let mut skipped_count: usize = 0;

    // Collect device accounts for two-pass processing.
    let device_accounts: Vec<&AccountInfo> = accounts_iter.collect();

    // First pass: pre-mark all existing node_segment_idx values as used in the
    // SegmentRoutingIds resource. This prevents collisions when the activator
    // manages SR IDs in-memory (use_onchain_allocation=false) and the on-chain
    // resource hasn't been updated to reflect those allocations.
    for device_account in &device_accounts {
        if device_account.owner != program_id {
            continue;
        }
        let device = match Device::try_from(&device_account.data.borrow()[..]) {
            Ok(d) => d,
            Err(_) => continue,
        };
        for iface in device.interfaces.iter() {
            let current = iface.into_current_version();
            if current.node_segment_idx > 0 {
                // Ignore error: ID may already be marked (idempotent pre-mark).
                let _ = allocate_specific_id(segment_routing_ids_account, current.node_segment_idx);
            }
            for fas in &current.flex_algo_node_segments {
                let _ = allocate_specific_id(segment_routing_ids_account, fas.node_segment_idx);
            }
        }
    }

    // Second pass: allocate new IDs for loopbacks missing this topology's segment.
    for device_account in &device_accounts {
        if device_account.owner != program_id {
            continue;
        }
        let mut device = match Device::try_from(&device_account.data.borrow()[..]) {
            Ok(d) => d,
            Err(_) => continue,
        };
        let mut modified = false;
        for iface in device.interfaces.iter_mut() {
            let iface_v2 = iface.into_current_version();
            if iface_v2.loopback_type != LoopbackType::Vpnv4 {
                continue;
            }
            // Skip if already has a segment for this topology (idempotent)
            if iface_v2
                .flex_algo_node_segments
                .iter()
                .any(|s| &s.topology == topology_key)
            {
                skipped_count += 1;
                continue;
            }
            let node_segment_idx = allocate_id(segment_routing_ids_account)?;
            match iface {
                Interface::V2(ref mut v2) => {
                    v2.flex_algo_node_segments.push(FlexAlgoNodeSegment {
                        topology: *topology_key,
                        node_segment_idx,
                    });
                }
                _ => {
                    let mut upgraded = iface.into_current_version();
                    upgraded.flex_algo_node_segments.push(FlexAlgoNodeSegment {
                        topology: *topology_key,
                        node_segment_idx,
                    });
                    *iface = Interface::V2(upgraded);
                }
            }
            modified = true;
            backfilled_count += 1;
        }
        if modified {
            try_acc_write(&device, device_account, payer_account, accounts)?;
        }
    }

    msg!(
        "TopologyBackfill: '{}' — {} loopback(s) backfilled, {} already had segment",
        value.name,
        backfilled_count,
        skipped_count
    );
    Ok(())
}
