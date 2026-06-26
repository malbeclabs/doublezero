use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::{get_globalstate_pda, get_permission_pda, get_resource_extension_pda, get_topology_pda},
    processors::{resource::allocate_id, validation::validate_program_account},
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        device::Device, globalstate::GlobalState, interface::LoopbackType,
        permission::permission_flags, topology::FlexAlgoNodeSegment,
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
pub struct AssignTopologyNodeSegmentsArgs {
    pub name: String,
}

// Keep the old name as a type alias for backward compatibility with existing serialized data.
// The on-wire format is identical (same Borsh layout, same instruction discriminant 110).
pub type TopologyBackfillArgs = AssignTopologyNodeSegmentsArgs;

/// Assign FlexAlgoNodeSegment entries on Vpnv4 loopbacks for a topology.
/// Idempotent — skips loopbacks that already have an entry for this topology.
///
/// Accounts layout:
/// [0]    topology PDA        (readonly — must already exist)
/// [1]    segment_routing_ids (writable, ResourceExtension)
/// [2]    globalstate         (readonly)
/// [3..n] Device accounts     (writable)
/// [n+1]  payer               (writable, signer, must hold TOPOLOGY_ADMIN)
/// [n+2]  system_program
/// [n+3]  permission          (readonly, optional — payer's Permission PDA)
///
/// Note: payer and system_program are the last two accounts (or the last two
/// before the optional Permission account). The SDK client always appends them
/// after the variable-length device list.
pub fn process_assign_topology_node_segments(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &AssignTopologyNodeSegmentsArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let topology_account = next_account_info(accounts_iter)?;
    let segment_routing_ids_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Collect remaining accounts. The SDK client always appends payer and
    // system_program at the end, after the variable-length device list, plus an
    // optional Permission account when one exists for the payer.
    let all_remaining: Vec<&AccountInfo> = accounts_iter.collect();
    if all_remaining.len() < 2 {
        msg!("AssignTopologyNodeSegments: expected at least payer and system_program accounts");
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    let n = all_remaining.len();
    // Detect an optional trailing Permission account. With it present the layout
    // is [devices.., payer, system, permission]; the payer would then be at n-3,
    // so the last account is a Permission account iff it matches that payer's PDA.
    let permission_account = if n >= 3 {
        let candidate_payer = all_remaining[n - 3];
        let (perm_pda, _) = get_permission_pda(program_id, candidate_payer.key);
        (all_remaining[n - 1].key == &perm_pda).then_some(all_remaining[n - 1])
    } else {
        None
    };
    let (payer_account, _system_program, device_accounts) = if permission_account.is_some() {
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

    #[cfg(test)]
    msg!("process_assign_topology_node_segments(name={})", value.name);

    if !payer_account.is_signer {
        msg!("AssignTopologyNodeSegments: payer must be a signer");
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Validate the Topology PDA (readonly). Backfill returns a recoverable
    // InvalidArgument when the topology does not yet exist, so we check the
    // PDA manually here instead of using validate_program_account! (which
    // would panic on the empty-account case).
    let (expected_topology_pda, _) = get_topology_pda(program_id, &value.name);
    if topology_account.key != &expected_topology_pda {
        msg!(
            "AssignTopologyNodeSegments: invalid topology PDA for name '{}'",
            value.name
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    if topology_account.data_is_empty() {
        msg!(
            "AssignTopologyNodeSegments: topology '{}' does not exist",
            value.name
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    assert_eq!(
        topology_account.owner, program_id,
        "Invalid Topology Account Owner"
    );

    // Validate SegmentRoutingIds resource PDA (writable — new IDs allocated here).
    let (expected_sr_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
    validate_program_account!(
        segment_routing_ids_account,
        program_id,
        writable = true,
        pda = &expected_sr_pda,
        "SegmentRoutingIds"
    );

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

    let topology_key = topology_account.key;
    let mut backfilled_count: usize = 0;
    let mut skipped_count: usize = 0;

    // Allocate new IDs for loopbacks missing this topology's segment.
    for device_account in device_accounts {
        msg!(
            "AssignTopologyNodeSegments: processing device {}",
            device_account.key
        );
        let mut device = Device::try_from(&device_account.data.borrow()[..])?;
        let mut modified = false;
        // `interfaces` is the source of truth for `flex_algo_node_segments`.
        // The custom Device serializer projects `interfaces` to the legacy
        // on-disk slot as V2, which intentionally drops segments — so we don't
        // mirror the change into the legacy in-memory vec here.
        for idx in 0..device.interfaces.len() {
            let new_iface = &device.interfaces[idx];
            if new_iface.loopback_type != LoopbackType::Vpnv4 {
                continue;
            }
            if new_iface
                .flex_algo_node_segments
                .iter()
                .any(|s| &s.topology == topology_key)
            {
                skipped_count += 1;
                continue;
            }
            // Allocate a fresh SR ID. Skip (keep as allocated) any ID that
            // conflicts with an existing base node_segment_idx — those IDs
            // remain marked used in the resource to avoid future collisions.
            let node_segment_idx = allocate_id(segment_routing_ids_account)?;
            let segment = FlexAlgoNodeSegment {
                topology: *topology_key,
                node_segment_idx,
            };
            device.interfaces[idx].flex_algo_node_segments.push(segment);
            modified = true;
            backfilled_count += 1;
        }
        if modified {
            try_acc_write(&device, device_account, payer_account, accounts)?;
        }
    }

    msg!(
        "AssignTopologyNodeSegments: '{}' — {} loopback(s) backfilled, {} already had segment",
        value.name,
        backfilled_count,
        skipped_count
    );
    Ok(())
}
