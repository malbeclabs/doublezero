//! Topology-domain instruction builders.
//!
//! All route through `authorize()` -> [`common::build_with_permission`]. The
//! topology PDA is derived from `args.name`. `clear_topology` and
//! `assign_topology_node_segments` are batched: a single-chunk builder plus a
//! `*_batched(...) -> Vec<Instruction>` convenience. The batch-size constants
//! account for the trailing `[payer, system]` the builder now owns, keeping each
//! transaction under Solana's 32-account cap.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_resource_extension_pda, get_topology_pda},
    processors::topology::{
        assign_node_segments::AssignTopologyNodeSegmentsArgs, clear::TopologyClearArgs,
        create::TopologyCreateArgs, delete::TopologyDeleteArgs,
    },
    resource::ResourceType,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// Max link accounts per `clear_topology` transaction (2 fixed + 16 links +
/// payer + system = 20 < 32).
pub const CLEAR_BATCH_SIZE: usize = 16;
/// Max device accounts per `assign_topology_node_segments` transaction (3 fixed +
/// 4 devices + payer + system = 9 < 32).
pub const BACKFILL_BATCH_SIZE: usize = 4;

/// `CreateTopology` (variant 107).
/// Accounts: `[topology, admin_group_bits, globalstate(readonly)]`.
pub fn create_topology(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: TopologyCreateArgs,
) -> Instruction {
    let (topology, _) = get_topology_pda(program_id, &args.name);
    let (admin_group_bits, _, _) =
        get_resource_extension_pda(program_id, ResourceType::AdminGroupBits);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateTopology(args),
        vec![
            AccountMeta::new(topology, false),
            AccountMeta::new(admin_group_bits, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `DeleteTopology` (variant 108). Accounts: `[topology, globalstate(readonly)]`.
pub fn delete_topology(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: TopologyDeleteArgs,
) -> Instruction {
    let (topology, _) = get_topology_pda(program_id, &args.name);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteTopology(args),
        vec![
            AccountMeta::new(topology, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `ClearTopology` (variant 109), single chunk.
/// Accounts: `[topology(writable), globalstate(readonly), link[i]...]`.
///
/// `topology` is writable: when the topology account still exists and at least
/// one link is cleared, the processor asserts `is_writable` and decrements its
/// `reference_count`. Passing it read-only fails that path; passing it writable
/// is harmless on the closed-topology path (the processor skips the write).
pub fn clear_topology(
    program_id: &Pubkey,
    payer: &Pubkey,
    links: &[Pubkey],
    args: TopologyClearArgs,
) -> Instruction {
    let (topology, _) = get_topology_pda(program_id, &args.name);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut accounts = vec![
        AccountMeta::new(topology, false),
        AccountMeta::new_readonly(globalstate, false),
    ];
    for link in links {
        accounts.push(AccountMeta::new(*link, false));
    }
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::ClearTopology(args),
        accounts,
        payer,
    )
}

/// Batched `clear_topology`: one instruction per [`CLEAR_BATCH_SIZE`] chunk of
/// `links` (empty `links` yields an empty vec).
pub fn clear_topology_batched(
    program_id: &Pubkey,
    payer: &Pubkey,
    links: &[Pubkey],
    args: TopologyClearArgs,
) -> Vec<Instruction> {
    links
        .chunks(CLEAR_BATCH_SIZE)
        .map(|chunk| clear_topology(program_id, payer, chunk, args.clone()))
        .collect()
}

/// `AssignTopologyNodeSegments` (variant 110), single chunk.
/// Accounts: `[topology(readonly), segment_routing_ids, globalstate(readonly), device[i]...]`.
pub fn assign_topology_node_segments(
    program_id: &Pubkey,
    payer: &Pubkey,
    devices: &[Pubkey],
    args: AssignTopologyNodeSegmentsArgs,
) -> Instruction {
    let (topology, _) = get_topology_pda(program_id, &args.name);
    let (segment_routing_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut accounts = vec![
        AccountMeta::new_readonly(topology, false),
        AccountMeta::new(segment_routing_ids, false),
        AccountMeta::new_readonly(globalstate, false),
    ];
    for device in devices {
        accounts.push(AccountMeta::new(*device, false));
    }
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::AssignTopologyNodeSegments(args),
        accounts,
        payer,
    )
}

/// Batched `assign_topology_node_segments`: one instruction per
/// [`BACKFILL_BATCH_SIZE`] chunk of `devices` (empty `devices` yields an empty vec).
pub fn assign_topology_node_segments_batched(
    program_id: &Pubkey,
    payer: &Pubkey,
    devices: &[Pubkey],
    args: AssignTopologyNodeSegmentsArgs,
) -> Vec<Instruction> {
    devices
        .chunks(BACKFILL_BATCH_SIZE)
        .map(|chunk| assign_topology_node_segments(program_id, payer, chunk, args.clone()))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_serviceability::state::topology::TopologyConstraint;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_create_topology() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let args = TopologyCreateArgs {
            name: "topo".to_string(),
            constraint: TopologyConstraint::default(),
        };
        let ix = create_topology(&pid, &payer, args);
        assert_eq!(ix.data[0], 107);
        let (topology, _) = get_topology_pda(&pid, "topo");
        let (admin_group_bits, _, _) =
            get_resource_extension_pda(&pid, ResourceType::AdminGroupBits);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(topology, false),
                AccountMeta::new(admin_group_bits, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_delete_topology() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let ix = delete_topology(
            &pid,
            &payer,
            TopologyDeleteArgs {
                name: "topo".to_string(),
            },
        );
        assert_eq!(ix.data[0], 108);
        let (topology, _) = get_topology_pda(&pid, "topo");
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(topology, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_clear_topology_single_and_batched() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let args = TopologyClearArgs {
            name: "topo".to_string(),
        };
        let links: Vec<Pubkey> = (0..CLEAR_BATCH_SIZE + 3)
            .map(|_| Pubkey::new_unique())
            .collect();

        let single = clear_topology(&pid, &payer, &links[..2], args.clone());
        assert_eq!(single.data[0], 109);
        let (topology, _) = get_topology_pda(&pid, "topo");
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            single.accounts,
            vec![
                // topology is writable: the processor decrements its reference_count.
                AccountMeta::new(topology, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(links[0], false),
                AccountMeta::new(links[1], false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );

        // 19 links -> 2 chunks (16 + 3).
        let batched = clear_topology_batched(&pid, &payer, &links, args);
        assert_eq!(batched.len(), 2);
        // First chunk: 2 fixed + 16 links + payer + system = 20.
        assert_eq!(batched[0].accounts.len(), 20);
        assert_eq!(batched[1].accounts.len(), 2 + 3 + 2);
    }

    #[test]
    fn test_assign_node_segments_single_and_batched() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let args = AssignTopologyNodeSegmentsArgs {
            name: "topo".to_string(),
        };
        let devices: Vec<Pubkey> = (0..BACKFILL_BATCH_SIZE + 1)
            .map(|_| Pubkey::new_unique())
            .collect();

        let single = assign_topology_node_segments(&pid, &payer, &devices[..1], args.clone());
        assert_eq!(single.data[0], 110);
        let (topology, _) = get_topology_pda(&pid, "topo");
        let (segment_routing_ids, _, _) =
            get_resource_extension_pda(&pid, ResourceType::SegmentRoutingIds);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            single.accounts,
            vec![
                AccountMeta::new_readonly(topology, false),
                AccountMeta::new(segment_routing_ids, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(devices[0], false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );

        // 5 devices -> 2 chunks (4 + 1).
        let batched = assign_topology_node_segments_batched(&pid, &payer, &devices, args);
        assert_eq!(batched.len(), 2);
        assert_eq!(batched[0].accounts.len(), 3 + 4 + 2);
        assert_eq!(batched[1].accounts.len(), 3 + 1 + 2);
    }
}
