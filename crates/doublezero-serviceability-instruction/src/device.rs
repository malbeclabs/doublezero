//! Device-domain instruction builders.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_device_pda, get_globalconfig_pda, get_globalstate_pda, get_resource_extension_pda,
        get_topology_pda,
    },
    processors::device::{
        create::DeviceCreateArgs,
        delete::DeviceDeleteArgs,
        interface::{
            create::DeviceInterfaceCreateArgs, DeviceInterfaceDeleteArgs, DeviceInterfaceUpdateArgs,
        },
        sethealth::DeviceSetHealthArgs,
        update::DeviceUpdateArgs,
    },
    resource::ResourceType,
    state::interface::LoopbackType,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateDevice` (variant 20).
///
/// Account layout (processor `next_account_info` order), before the trailing
/// `[payer, system]` appended by `common::build_with_permission`:
///
/// ```text
/// device                (writable)  — PDA get_device_pda(device_index)
/// contributor           (writable)
/// location              (writable)
/// exchange              (writable)
/// globalstate           (writable)
/// globalconfig          (writable)
/// tunnel_ids resource   (writable)  — ResourceType::TunnelIds(device, 0)
/// dz_prefix_block[i]    (writable)  — one per args.dz_prefixes entry
/// ```
///
/// The writable flags mirror the existing SDK command exactly (e.g. `globalconfig`
/// is only read by the processor but is sent writable there too). Byte-parity is
/// deliberate — the golden fixtures freeze this layout — so the flags are kept as
/// the SDK emits them rather than tightened.
///
/// The `dz_prefix` blocks and `args.resource_count` are produced from the same
/// loop, so the declared count can never disagree with the account list.
///
/// `device_index` is the **new** device's index: the caller passes
/// `globalstate.account_index + 1`, not the raw current value.
pub fn create_device(
    program_id: &Pubkey,
    payer: &Pubkey,
    contributor: &Pubkey,
    location: &Pubkey,
    exchange: &Pubkey,
    device_index: u128,
    mut args: DeviceCreateArgs,
) -> Instruction {
    let (device, _) = get_device_pda(program_id, device_index);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (globalconfig, _) = get_globalconfig_pda(program_id);
    let (tunnel_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::TunnelIds(device, 0));

    let mut accounts = vec![
        AccountMeta::new(device, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(*location, false),
        AccountMeta::new(*exchange, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(globalconfig, false),
        AccountMeta::new(tunnel_ids, false),
    ];

    let dz_prefix_count = args.dz_prefixes.len();
    for idx in 0..dz_prefix_count {
        let (dz_prefix, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DzPrefixBlock(device, idx));
        accounts.push(AccountMeta::new(dz_prefix, false));
    }

    // One TunnelIds account plus one DzPrefixBlock per advertised prefix, derived
    // from the same loop that produced the accounts above. The count is bounded
    // by the transaction's account budget, so overflow is unreachable in practice;
    // panicking is strictly better than emitting a `resource_count` that disagrees
    // with the account list — the exact invariant this crate exists to protect.
    let resource_total = 1 + dz_prefix_count;
    args.resource_count =
        u8::try_from(resource_total).expect("device resource_count exceeds u8::MAX");

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateDevice(args),
        accounts,
        payer,
    )
}

/// How `delete_device` closes the device's resource accounts. Selecting the
/// path explicitly (rather than inferring it from an empty slice) prevents an
/// activated device from silently getting the legacy layout.
pub enum DeviceDeleteResources<'a> {
    /// Never-activated device with no live resource accounts.
    Legacy,
    /// Activated device: atomically close its resource accounts. `owners[i]` is
    /// the onchain-read owner of resource `i` (idx 0 = `TunnelIds(device, 0)`,
    /// idx 1.. = `DzPrefixBlock(device, idx - 1)`), not offline-derivable.
    Atomic {
        location: &'a Pubkey,
        exchange: &'a Pubkey,
        owners: &'a [Pubkey],
        device_owner: &'a Pubkey,
    },
}

/// `DeleteDevice` (variant 26).
///
/// Two layouts, selected by [`DeviceDeleteResources`]:
///
/// - [`DeviceDeleteResources::Legacy`]:
///   ```text
///   device       (writable)
///   contributor  (writable)
///   globalstate  (writable)
///   ```
/// - [`DeviceDeleteResources::Atomic`]:
///   ```text
///   device                (writable)
///   contributor           (writable)
///   globalstate           (writable)
///   location              (writable)
///   exchange              (writable)
///   resource[i]           (writable)  — idx 0: TunnelIds(device, 0);
///                                        idx 1..: DzPrefixBlock(device, idx-1)
///   resource_owner[i]     (writable)  — the onchain owner of resource[i]
///   device_owner          (writable)
///   ```
///
/// `owners.len()` drives both the resource-PDA loop and `args.resource_count`.
///
/// The writable flags mirror the existing SDK command (e.g. `globalstate` is sent
/// writable there too, even though the processor validates it `writable = false`).
/// Byte-parity is deliberate and frozen by the fixtures.
///
/// `process_delete_device` routes through `authorize()` (NETWORK_ADMIN, for the
/// non-contributor override), so this builder is assigned to
/// `common::build_with_permission` and will carry a trailing Permission PDA once
/// the permission model is activated (deferred today, like every other assigned
/// builder).
pub fn delete_device(
    program_id: &Pubkey,
    payer: &Pubkey,
    device: &Pubkey,
    contributor: &Pubkey,
    resources: DeviceDeleteResources,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);

    let mut accounts = vec![
        AccountMeta::new(*device, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(globalstate, false),
    ];

    let (location, exchange, owners, device_owner) = match resources {
        DeviceDeleteResources::Legacy => {
            return common::build_with_permission(
                program_id,
                DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs::default()),
                accounts,
                payer,
            );
        }
        DeviceDeleteResources::Atomic {
            location,
            exchange,
            owners,
            device_owner,
        } => (location, exchange, owners, device_owner),
    };

    accounts.push(AccountMeta::new(*location, false));
    accounts.push(AccountMeta::new(*exchange, false));
    // Resource PDAs, in the order the processor consumes them.
    for idx in 0..owners.len() {
        let resource_type = if idx == 0 {
            ResourceType::TunnelIds(*device, 0)
        } else {
            ResourceType::DzPrefixBlock(*device, idx - 1)
        };
        let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);
        accounts.push(AccountMeta::new(pda, false));
    }
    // Then the owner of each resource account, in the same order.
    for owner in owners {
        accounts.push(AccountMeta::new(*owner, false));
    }
    accounts.push(AccountMeta::new(*device_owner, false));

    // Bounded by the transaction's account budget, so overflow is unreachable;
    // panicking is strictly better than emitting a `resource_count` that disagrees
    // with the resource account list.
    let resource_count =
        u8::try_from(owners.len()).expect("device delete resource_count exceeds u8::MAX");
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs { resource_count }),
        accounts,
        payer,
    )
}

/// `UpdateDevice` (variant 23).
///
/// Account layout (processor `next_account_info` order), before the trailing
/// accounts appended by [`common::build_with_permission`]:
///
/// ```text
/// device                (writable)
/// current_contributor   (writable)  — device.contributor_pk
/// current_location      (writable)  — device.location_pk
/// new_location          (writable)  — the new location, or current if unchanged
/// globalstate           (writable)
/// // present only when args.dz_prefixes.is_some():
/// globalconfig          (writable)
/// resource[i]           (writable)  — idx 0: TunnelIds(device, 0);
///                                      idx 1..: DzPrefixBlock(device, idx-1),
///                                      looped 0..=max(old, new) dz_prefix count
/// ```
///
/// The dz_prefix resource block covers `max(old, new)` prefixes, so the old count
/// (RPC-read from the device) is required alongside the new count in
/// `args.dz_prefixes`. `args.resource_count` is written from the same loop.
#[allow(clippy::too_many_arguments)]
pub fn update_device(
    program_id: &Pubkey,
    payer: &Pubkey,
    device: &Pubkey,
    current_contributor: &Pubkey,
    current_location: &Pubkey,
    new_location: &Pubkey,
    old_dz_prefix_count: usize,
    mut args: DeviceUpdateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut accounts = vec![
        AccountMeta::new(*device, false),
        AccountMeta::new(*current_contributor, false),
        AccountMeta::new(*current_location, false),
        AccountMeta::new(*new_location, false),
        AccountMeta::new(globalstate, false),
    ];

    let mut resource_count = 0usize;
    if let Some(dz_prefixes) = args.dz_prefixes.as_ref() {
        let (globalconfig, _) = get_globalconfig_pda(program_id);
        accounts.push(AccountMeta::new(globalconfig, false));
        let max_count = old_dz_prefix_count.max(dz_prefixes.len());
        for idx in 0..=max_count {
            let resource_type = if idx == 0 {
                ResourceType::TunnelIds(*device, 0)
            } else {
                ResourceType::DzPrefixBlock(*device, idx - 1)
            };
            let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);
            accounts.push(AccountMeta::new(pda, false));
        }
        resource_count = max_count + 1;
    }
    args.resource_count = resource_count;

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateDevice(args),
        accounts,
        payer,
    )
}

/// `SetDeviceHealth` (variant 83).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// device       (writable)
/// globalstate  (writable)
/// ```
pub fn set_device_health(
    program_id: &Pubkey,
    payer: &Pubkey,
    device: &Pubkey,
    args: DeviceSetHealthArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let accounts = vec![
        AccountMeta::new(*device, false),
        AccountMeta::new(globalstate, false),
    ];
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SetDeviceHealth(args),
        accounts,
        payer,
    )
}

/// `CreateDeviceInterface` (variant 73).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// device                (writable)
/// contributor           (writable)  — device.contributor_pk
/// globalstate           (writable)
/// device_tunnel_block   (writable)  — ResourceType::DeviceTunnelBlock
/// segment_routing_ids   (writable)  — ResourceType::SegmentRoutingIds
/// topology[i]           (readonly)  — one per topology_name, ONLY for Vpnv4 loopbacks
/// ```
///
/// Topology PDAs are appended (and `args.topology_count` set) only when
/// `args.loopback_type == Vpnv4`; otherwise `topology_names` is ignored.
pub fn create_device_interface(
    program_id: &Pubkey,
    payer: &Pubkey,
    device: &Pubkey,
    contributor: &Pubkey,
    topology_names: &[String],
    mut args: DeviceInterfaceCreateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (device_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
    let mut accounts = vec![
        AccountMeta::new(*device, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(device_tunnel_block, false),
        AccountMeta::new(segment_routing_ids, false),
    ];

    let is_vpnv4 = args.loopback_type == LoopbackType::Vpnv4;
    if is_vpnv4 {
        for name in topology_names {
            let (topology, _) = get_topology_pda(program_id, name);
            accounts.push(AccountMeta::new_readonly(topology, false));
        }
    }
    // The processor rejects `use_onchain_allocation == false` as its first
    // statement (interface/create.rs), and `false` is the struct default — a
    // caller-supplied value here can only ever fail. This builder owns onchain
    // allocation, so it forces the flag (as the SDK command does).
    args.use_onchain_allocation = true;

    let topology_count = if is_vpnv4 { topology_names.len() } else { 0 };
    // panicking is strictly better than emitting a `topology_count` that
    // disagrees with the account list (matches the R0 builders in this file)
    args.topology_count =
        u8::try_from(topology_count).expect("device interface topology_count exceeds u8::MAX");

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(args),
        accounts,
        payer,
    )
}

/// `DeleteDeviceInterface` (variant 74).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// device                (writable)
/// contributor           (writable)  — device.contributor_pk
/// globalstate           (writable)
/// device_tunnel_block   (writable)  — ResourceType::DeviceTunnelBlock
/// segment_routing_ids   (writable)  — ResourceType::SegmentRoutingIds
/// ```
pub fn delete_device_interface(
    program_id: &Pubkey,
    payer: &Pubkey,
    device: &Pubkey,
    contributor: &Pubkey,
    mut args: DeviceInterfaceDeleteArgs,
) -> Instruction {
    // The processor rejects `use_onchain_deallocation == false` as its first
    // statement (interface/delete.rs), and `false` is the struct default — a
    // caller-supplied value here can only ever fail. Force it, as the SDK does.
    args.use_onchain_deallocation = true;

    let (globalstate, _) = get_globalstate_pda(program_id);
    let (device_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
    let (segment_routing_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
    let accounts = vec![
        AccountMeta::new(*device, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(device_tunnel_block, false),
        AccountMeta::new(segment_routing_ids, false),
    ];
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteDeviceInterface(args),
        accounts,
        payer,
    )
}

/// `UpdateDeviceInterface` (variant 76).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// device                (writable)
/// contributor           (writable)  — device.contributor_pk
/// globalstate           (writable)
/// // present when args.node_segment_idx.is_some() OR topology_names.is_some():
/// segment_routing_ids   (writable)  — ResourceType::SegmentRoutingIds
/// // present when topology_names.is_some():
/// topology[i]           (readonly)  — one per topology_name
/// ```
///
/// `topology_names` is `None` to leave the flex-algo topology set alone,
/// `Some(&[])` to clear it, or `Some(names)` to set it exactly.
/// `args.update_topologies` / `args.topology_count` are written from that choice.
pub fn update_device_interface(
    program_id: &Pubkey,
    payer: &Pubkey,
    device: &Pubkey,
    contributor: &Pubkey,
    topology_names: Option<&[String]>,
    mut args: DeviceInterfaceUpdateArgs,
) -> Instruction {
    let update_topologies = topology_names.is_some();
    let topology_count = topology_names.map_or(0, <[String]>::len);
    args.update_topologies = update_topologies;
    // panicking is strictly better than emitting a `topology_count` that
    // disagrees with the account list (matches the R0 builders in this file)
    args.topology_count =
        u8::try_from(topology_count).expect("device interface topology_count exceeds u8::MAX");

    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut accounts = vec![
        AccountMeta::new(*device, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(globalstate, false),
    ];

    if args.node_segment_idx.is_some() || update_topologies {
        let (segment_routing_ids, _, _) =
            get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
        accounts.push(AccountMeta::new(segment_routing_ids, false));
    }
    if let Some(names) = topology_names {
        for name in names {
            let (topology, _) = get_topology_pda(program_id, name);
            accounts.push(AccountMeta::new_readonly(topology, false));
        }
    }

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateDeviceInterface(args),
        accounts,
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_serviceability::state::device::DeviceType;
    use solana_system_interface::program as system_program;

    fn program_id() -> Pubkey {
        Pubkey::new_unique()
    }

    #[test]
    fn test_create_device_accounts_and_tag() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let location = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();

        let args = DeviceCreateArgs {
            code: "dev1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.0/8".parse().unwrap(),
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
            resource_count: 0,
        };

        let ix = create_device(&pid, &payer, &contributor, &location, &exchange, 1, args);

        // Tag byte for CreateDevice.
        assert_eq!(ix.data[0], 20);
        assert_eq!(ix.program_id, pid);

        let (device, _) = get_device_pda(&pid, 1);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (globalconfig, _) = get_globalconfig_pda(&pid);
        let (tunnel_ids, _, _) =
            get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz_prefix0, _, _) =
            get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));

        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(location, false),
                AccountMeta::new(exchange, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(globalconfig, false),
                AccountMeta::new(tunnel_ids, false),
                AccountMeta::new(dz_prefix0, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_create_device_writes_resource_count() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        // Two prefixes -> resource_count = 1 (TunnelIds) + 2 = 3.
        let args = DeviceCreateArgs {
            code: "dev1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.0/8,11.0.0.0/8".parse().unwrap(),
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
            resource_count: 0,
        };
        let ix = create_device(
            &pid,
            &payer,
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            1,
            args,
        );
        let decoded = DoubleZeroInstruction::unpack(&ix.data).unwrap();
        match decoded {
            DoubleZeroInstruction::CreateDevice(a) => assert_eq!(a.resource_count, 3),
            other => panic!("unexpected variant: {other:?}"),
        }
        // account list: 7 fixed + 2 dz_prefix + payer + system = 11
        assert_eq!(ix.accounts.len(), 11);
    }

    #[test]
    fn test_delete_device_legacy() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();

        let ix = delete_device(
            &pid,
            &payer,
            &device,
            &contributor,
            DeviceDeleteResources::Legacy,
        );

        assert_eq!(ix.data[0], 26);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_delete_device_atomic() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let location = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();
        let res_owner = Pubkey::new_unique();
        let device_owner = Pubkey::new_unique();

        // One TunnelIds + one DzPrefixBlock -> two resource owners.
        let ix = delete_device(
            &pid,
            &payer,
            &device,
            &contributor,
            DeviceDeleteResources::Atomic {
                location: &location,
                exchange: &exchange,
                owners: &[res_owner, res_owner],
                device_owner: &device_owner,
            },
        );

        let decoded = DoubleZeroInstruction::unpack(&ix.data).unwrap();
        match decoded {
            DoubleZeroInstruction::DeleteDevice(a) => assert_eq!(a.resource_count, 2),
            other => panic!("unexpected variant: {other:?}"),
        }

        let (globalstate, _) = get_globalstate_pda(&pid);
        let (tunnel_ids, _, _) =
            get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz_prefix0, _, _) =
            get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(location, false),
                AccountMeta::new(exchange, false),
                AccountMeta::new(tunnel_ids, false),
                AccountMeta::new(dz_prefix0, false),
                AccountMeta::new(res_owner, false),
                AccountMeta::new(res_owner, false),
                AccountMeta::new(device_owner, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_update_device_with_dz_prefixes() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let cc = Pubkey::new_unique();
        let cl = Pubkey::new_unique();
        let nl = Pubkey::new_unique();
        let args = DeviceUpdateArgs {
            dz_prefixes: Some("10.0.0.0/8".parse().unwrap()),
            ..Default::default()
        };
        // old count 1, new count 1 -> max 1 -> TunnelIds + DzPrefix0, resource_count 2.
        let ix = update_device(&pid, &payer, &device, &cc, &cl, &nl, 1, args);
        assert_eq!(ix.data[0], 23);
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::UpdateDevice(a) => assert_eq!(a.resource_count, 2),
            other => panic!("unexpected: {other:?}"),
        }
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (globalconfig, _) = get_globalconfig_pda(&pid);
        let (tunnel_ids, _, _) =
            get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz0, _, _) = get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(cc, false),
                AccountMeta::new(cl, false),
                AccountMeta::new(nl, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(globalconfig, false),
                AccountMeta::new(tunnel_ids, false),
                AccountMeta::new(dz0, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_update_device_no_resources() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let cc = Pubkey::new_unique();
        let cl = Pubkey::new_unique();
        let nl = Pubkey::new_unique();
        // No dz_prefixes -> no globalconfig/resources regardless of old count.
        let ix = update_device(
            &pid,
            &payer,
            &device,
            &cc,
            &cl,
            &nl,
            3,
            DeviceUpdateArgs::default(),
        );
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(cc, false),
                AccountMeta::new(cl, false),
                AccountMeta::new(nl, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::UpdateDevice(a) => assert_eq!(a.resource_count, 0),
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn test_set_device_health() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let ix = set_device_health(&pid, &payer, &device, DeviceSetHealthArgs::default());
        assert_eq!(ix.data[0], 83);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_create_device_interface_non_vpnv4_ignores_topologies() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        // Default loopback_type is None (non-Vpnv4); topology_names must be ignored.
        let ix = create_device_interface(
            &pid,
            &payer,
            &device,
            &contributor,
            &["TOPO".to_string()],
            DeviceInterfaceCreateArgs::default(),
        );
        assert_eq!(ix.data[0], 73);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (dtb, _, _) = get_resource_extension_pda(&pid, ResourceType::DeviceTunnelBlock);
        let (sri, _, _) = get_resource_extension_pda(&pid, ResourceType::SegmentRoutingIds);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(dtb, false),
                AccountMeta::new(sri, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::CreateDeviceInterface(a) => {
                assert_eq!(a.topology_count, 0);
                // The processor rejects use_onchain_allocation == false; the
                // builder must force it regardless of the caller's default.
                assert!(a.use_onchain_allocation);
            }
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn test_create_device_interface_vpnv4_appends_topologies() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let args = DeviceInterfaceCreateArgs {
            loopback_type: LoopbackType::Vpnv4,
            ..Default::default()
        };
        let ix = create_device_interface(
            &pid,
            &payer,
            &device,
            &contributor,
            &["TOPO-A".to_string(), "TOPO-B".to_string()],
            args,
        );
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (dtb, _, _) = get_resource_extension_pda(&pid, ResourceType::DeviceTunnelBlock);
        let (sri, _, _) = get_resource_extension_pda(&pid, ResourceType::SegmentRoutingIds);
        let (ta, _) = get_topology_pda(&pid, "TOPO-A");
        let (tb, _) = get_topology_pda(&pid, "TOPO-B");
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(dtb, false),
                AccountMeta::new(sri, false),
                AccountMeta::new_readonly(ta, false),
                AccountMeta::new_readonly(tb, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::CreateDeviceInterface(a) => {
                assert_eq!(a.topology_count, 2);
                assert!(a.use_onchain_allocation);
            }
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn test_delete_device_interface() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let ix = delete_device_interface(
            &pid,
            &payer,
            &device,
            &contributor,
            DeviceInterfaceDeleteArgs::default(),
        );
        assert_eq!(ix.data[0], 74);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (dtb, _, _) = get_resource_extension_pda(&pid, ResourceType::DeviceTunnelBlock);
        let (sri, _, _) = get_resource_extension_pda(&pid, ResourceType::SegmentRoutingIds);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(dtb, false),
                AccountMeta::new(sri, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::DeleteDeviceInterface(a) => {
                // The processor rejects use_onchain_deallocation == false; the
                // builder must force it regardless of the caller's default.
                assert!(a.use_onchain_deallocation);
            }
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn test_update_device_interface_none_leaves_topologies() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let ix = update_device_interface(
            &pid,
            &payer,
            &device,
            &contributor,
            None,
            DeviceInterfaceUpdateArgs::default(),
        );
        assert_eq!(ix.data[0], 76);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_update_device_interface_node_segment_adds_seg() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let args = DeviceInterfaceUpdateArgs {
            node_segment_idx: Some(42),
            ..Default::default()
        };
        let ix = update_device_interface(&pid, &payer, &device, &contributor, None, args);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (sri, _, _) = get_resource_extension_pda(&pid, ResourceType::SegmentRoutingIds);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(sri, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_update_device_interface_topologies_add_seg_and_topos() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let names = ["TOPO-A".to_string(), "TOPO-B".to_string()];
        let ix = update_device_interface(
            &pid,
            &payer,
            &device,
            &contributor,
            Some(&names),
            DeviceInterfaceUpdateArgs::default(),
        );
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (sri, _, _) = get_resource_extension_pda(&pid, ResourceType::SegmentRoutingIds);
        let (ta, _) = get_topology_pda(&pid, "TOPO-A");
        let (tb, _) = get_topology_pda(&pid, "TOPO-B");
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(sri, false),
                AccountMeta::new_readonly(ta, false),
                AccountMeta::new_readonly(tb, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::UpdateDeviceInterface(a) => {
                assert_eq!(a.topology_count, 2);
                assert!(a.update_topologies);
            }
            other => panic!("unexpected: {other:?}"),
        }
    }
}
