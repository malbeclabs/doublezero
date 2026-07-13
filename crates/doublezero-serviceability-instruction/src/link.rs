//! Link-domain instruction builders.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_globalstate_pda, get_link_pda, get_resource_extension_pda, get_topology_pda,
        UNICAST_DEFAULT_TOPOLOGY_NAME,
    },
    processors::link::{
        accept::LinkAcceptArgs, create::LinkCreateArgs, delete::LinkDeleteArgs,
        sethealth::LinkSetHealthArgs, update::LinkUpdateArgs,
    },
    resource::ResourceType,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateLink` (variant 28).
///
/// Account layout (processor `next_account_info` order), before the trailing
/// `[payer, system]` appended by `common::build_with_permission`:
///
/// ```text
/// link                       (writable)  — PDA get_link_pda(link_index)
/// contributor                (writable)
/// side_a                     (writable)
/// side_z                     (writable)
/// globalstate                (writable)
/// unicast_default_topology   (writable)  — get_topology_pda("unicast-default")
/// device_tunnel_block        (writable)  — ResourceType::DeviceTunnelBlock
/// link_ids                   (writable)  — ResourceType::LinkIds
/// ```
pub fn create_link(
    program_id: &Pubkey,
    payer: &Pubkey,
    contributor: &Pubkey,
    side_a: &Pubkey,
    side_z: &Pubkey,
    link_index: u128,
    args: LinkCreateArgs,
) -> Instruction {
    let (link, _) = get_link_pda(program_id, link_index);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (device_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
    let (link_ids, _, _) = get_resource_extension_pda(program_id, ResourceType::LinkIds);
    let (unicast_default_topology, _) = get_topology_pda(program_id, UNICAST_DEFAULT_TOPOLOGY_NAME);

    let accounts = vec![
        AccountMeta::new(link, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(*side_a, false),
        AccountMeta::new(*side_z, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(unicast_default_topology, false),
        AccountMeta::new(device_tunnel_block, false),
        AccountMeta::new(link_ids, false),
    ];

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateLink(args),
        accounts,
        payer,
    )
}

/// `AcceptLink` (variant 66).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// link                  (writable)
/// contributor           (writable)  — device_z.contributor_pk
/// side_z                (writable)  — link.side_z_pk
/// globalstate           (writable)
/// side_a                (writable)  — link.side_a_pk
/// device_tunnel_block   (writable)  — ResourceType::DeviceTunnelBlock
/// link_ids              (writable)  — ResourceType::LinkIds
/// ```
pub fn accept_link(
    program_id: &Pubkey,
    payer: &Pubkey,
    link: &Pubkey,
    contributor: &Pubkey,
    side_z: &Pubkey,
    side_a: &Pubkey,
    mut args: LinkAcceptArgs,
) -> Instruction {
    // The processor rejects `use_onchain_allocation == false` as its first
    // statement (accept.rs), and `false` is the struct default — a caller-supplied
    // value here can only ever fail. This builder owns onchain allocation, so it
    // forces the flag (as the SDK command does).
    args.use_onchain_allocation = true;

    let (globalstate, _) = get_globalstate_pda(program_id);
    let (device_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
    let (link_ids, _, _) = get_resource_extension_pda(program_id, ResourceType::LinkIds);
    let accounts = vec![
        AccountMeta::new(*link, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(*side_z, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(*side_a, false),
        AccountMeta::new(device_tunnel_block, false),
        AccountMeta::new(link_ids, false),
    ];
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::AcceptLink(args),
        accounts,
        payer,
    )
}

/// Which contributor authority is updating a link, selecting the account preamble
/// `UpdateLink` uses (the processor accepts either the link's own contributor or
/// side-Z's contributor). The caller resolves this from onchain state.
pub enum LinkUpdateAuthority<'a> {
    /// The link's own contributor. Preamble: `[link, contributor, globalstate]`.
    Contributor { contributor: &'a Pubkey },
    /// Side-Z's contributor. Preamble: `[link, contributor, side_z, globalstate]`.
    SideZContributor { contributor: &'a Pubkey },
}

/// `UpdateLink` (variant 31).
///
/// Account layout, before the trailing accounts (all sections after the preamble
/// are conditional):
///
/// ```text
/// // preamble — see LinkUpdateAuthority:
/// link, [contributor | contributor, side_z], globalstate   (all writable)
/// // when args.tunnel_net.is_some():
/// side_a, side_z                                            (writable)
/// // when args.tunnel_id.is_some() || args.tunnel_net.is_some():
/// device_tunnel_block, link_ids                             (writable)
/// // when args.link_topologies.is_some():
/// topology_union[i]                                         (writable)
/// ```
///
/// `topology_union` is the union of the link's current `link_topologies` (RPC-read)
/// and the requested new set; it is appended only when `args.link_topologies` is
/// `Some`. `args.use_onchain_allocation` is set to whether tunnel resources are
/// being updated.
#[allow(clippy::too_many_arguments)]
pub fn update_link(
    program_id: &Pubkey,
    payer: &Pubkey,
    link: &Pubkey,
    authority: LinkUpdateAuthority,
    side_a: &Pubkey,
    side_z: &Pubkey,
    topology_union: &[Pubkey],
    mut args: LinkUpdateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut accounts = match authority {
        LinkUpdateAuthority::SideZContributor { contributor } => vec![
            AccountMeta::new(*link, false),
            AccountMeta::new(*contributor, false),
            AccountMeta::new(*side_z, false),
            AccountMeta::new(globalstate, false),
        ],
        LinkUpdateAuthority::Contributor { contributor } => vec![
            AccountMeta::new(*link, false),
            AccountMeta::new(*contributor, false),
            AccountMeta::new(globalstate, false),
        ],
    };

    if args.tunnel_net.is_some() {
        accounts.push(AccountMeta::new(*side_a, false));
        accounts.push(AccountMeta::new(*side_z, false));
    }

    let updating_tunnel_resources = args.tunnel_id.is_some() || args.tunnel_net.is_some();
    if updating_tunnel_resources {
        let (device_tunnel_block, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
        let (link_ids, _, _) = get_resource_extension_pda(program_id, ResourceType::LinkIds);
        accounts.push(AccountMeta::new(device_tunnel_block, false));
        accounts.push(AccountMeta::new(link_ids, false));
    }
    args.use_onchain_allocation = updating_tunnel_resources;

    if args.link_topologies.is_some() {
        for topology in topology_union {
            accounts.push(AccountMeta::new(*topology, false));
        }
    }

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateLink(args),
        accounts,
        payer,
    )
}

/// `DeleteLink` (variant 34).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// link                  (writable)
/// contributor           (writable)  — link.contributor_pk
/// globalstate           (writable)
/// side_a                (writable)  — link.side_a_pk
/// side_z                (writable)  — link.side_z_pk
/// device_tunnel_block   (writable)  — ResourceType::DeviceTunnelBlock
/// link_ids              (writable)  — ResourceType::LinkIds
/// owner                 (writable)  — link.owner
/// topology[i]           (writable)  — one per link.link_topologies entry
/// ```
#[allow(clippy::too_many_arguments)]
pub fn delete_link(
    program_id: &Pubkey,
    payer: &Pubkey,
    link: &Pubkey,
    contributor: &Pubkey,
    side_a: &Pubkey,
    side_z: &Pubkey,
    owner: &Pubkey,
    link_topologies: &[Pubkey],
    mut args: LinkDeleteArgs,
) -> Instruction {
    // The processor rejects `use_onchain_deallocation == false` as its first
    // statement (delete.rs), and `false` is the struct default — a caller-supplied
    // value here can only ever fail. Force it, as the SDK does.
    args.use_onchain_deallocation = true;

    let (globalstate, _) = get_globalstate_pda(program_id);
    let (device_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
    let (link_ids, _, _) = get_resource_extension_pda(program_id, ResourceType::LinkIds);
    let mut accounts = vec![
        AccountMeta::new(*link, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(*side_a, false),
        AccountMeta::new(*side_z, false),
        AccountMeta::new(device_tunnel_block, false),
        AccountMeta::new(link_ids, false),
        AccountMeta::new(*owner, false),
    ];
    for topology in link_topologies {
        accounts.push(AccountMeta::new(*topology, false));
    }
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteLink(args),
        accounts,
        payer,
    )
}

/// `SetLinkHealth` (variant 84).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// link         (writable)
/// globalstate  (writable)
/// ```
pub fn set_link_health(
    program_id: &Pubkey,
    payer: &Pubkey,
    link: &Pubkey,
    args: LinkSetHealthArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let accounts = vec![
        AccountMeta::new(*link, false),
        AccountMeta::new(globalstate, false),
    ];
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SetLinkHealth(args),
        accounts,
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_serviceability::state::link::LinkLinkType;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_create_link_accounts_and_tag() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let side_a = Pubkey::new_unique();
        let side_z = Pubkey::new_unique();

        let args = LinkCreateArgs {
            code: "link1".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1_000_000,
            jitter_ns: 100_000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: None,
            use_onchain_allocation: true,
        };

        let ix = create_link(&pid, &payer, &contributor, &side_a, &side_z, 1, args);

        assert_eq!(ix.data[0], 28);
        assert_eq!(ix.program_id, pid);

        let (link, _) = get_link_pda(&pid, 1);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (device_tunnel_block, _, _) =
            get_resource_extension_pda(&pid, ResourceType::DeviceTunnelBlock);
        let (link_ids, _, _) = get_resource_extension_pda(&pid, ResourceType::LinkIds);
        let (unicast_default, _) = get_topology_pda(&pid, UNICAST_DEFAULT_TOPOLOGY_NAME);

        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(link, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(side_a, false),
                AccountMeta::new(side_z, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(unicast_default, false),
                AccountMeta::new(device_tunnel_block, false),
                AccountMeta::new(link_ids, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_accept_link_accounts_and_tag() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let link = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let side_z = Pubkey::new_unique();
        let side_a = Pubkey::new_unique();

        let ix = accept_link(
            &pid,
            &payer,
            &link,
            &contributor,
            &side_z,
            &side_a,
            LinkAcceptArgs::default(),
        );
        assert_eq!(ix.data[0], 66);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (device_tunnel_block, _, _) =
            get_resource_extension_pda(&pid, ResourceType::DeviceTunnelBlock);
        let (link_ids, _, _) = get_resource_extension_pda(&pid, ResourceType::LinkIds);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(link, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(side_z, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(side_a, false),
                AccountMeta::new(device_tunnel_block, false),
                AccountMeta::new(link_ids, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::AcceptLink(a) => {
                // The processor rejects use_onchain_allocation == false; the
                // builder must force it regardless of the caller's default.
                assert!(a.use_onchain_allocation);
            }
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn test_update_link_side_z_preamble() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let link = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let side_a = Pubkey::new_unique();
        let side_z = Pubkey::new_unique();

        // No tunnel fields, no topologies -> just the 4-account side-Z preamble.
        let ix = update_link(
            &pid,
            &payer,
            &link,
            LinkUpdateAuthority::SideZContributor {
                contributor: &contributor,
            },
            &side_a,
            &side_z,
            &[],
            LinkUpdateArgs::default(),
        );
        assert_eq!(ix.data[0], 31);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(link, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(side_z, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_update_link_contributor_tunnel_net_and_topologies() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let link = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let side_a = Pubkey::new_unique();
        let side_z = Pubkey::new_unique();
        let topo = Pubkey::new_unique();

        let args = LinkUpdateArgs {
            tunnel_net: Some("10.0.0.0/21".parse().unwrap()),
            link_topologies: Some(vec![topo]),
            ..Default::default()
        };
        let ix = update_link(
            &pid,
            &payer,
            &link,
            LinkUpdateAuthority::Contributor {
                contributor: &contributor,
            },
            &side_a,
            &side_z,
            &[topo],
            args,
        );
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (device_tunnel_block, _, _) =
            get_resource_extension_pda(&pid, ResourceType::DeviceTunnelBlock);
        let (link_ids, _, _) = get_resource_extension_pda(&pid, ResourceType::LinkIds);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(link, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(side_a, false),
                AccountMeta::new(side_z, false),
                AccountMeta::new(device_tunnel_block, false),
                AccountMeta::new(link_ids, false),
                AccountMeta::new(topo, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        // use_onchain_allocation is derived from whether tunnel resources are updated.
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::UpdateLink(a) => assert!(a.use_onchain_allocation),
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn test_delete_link_with_topologies() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let link = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let side_a = Pubkey::new_unique();
        let side_z = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let topo = Pubkey::new_unique();

        let ix = delete_link(
            &pid,
            &payer,
            &link,
            &contributor,
            &side_a,
            &side_z,
            &owner,
            &[topo],
            LinkDeleteArgs::default(),
        );
        assert_eq!(ix.data[0], 34);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (device_tunnel_block, _, _) =
            get_resource_extension_pda(&pid, ResourceType::DeviceTunnelBlock);
        let (link_ids, _, _) = get_resource_extension_pda(&pid, ResourceType::LinkIds);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(link, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(side_a, false),
                AccountMeta::new(side_z, false),
                AccountMeta::new(device_tunnel_block, false),
                AccountMeta::new(link_ids, false),
                AccountMeta::new(owner, false),
                AccountMeta::new(topo, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::DeleteLink(a) => {
                // The processor rejects use_onchain_deallocation == false; the
                // builder must force it regardless of the caller's default.
                assert!(a.use_onchain_deallocation);
            }
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn test_set_link_health() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let link = Pubkey::new_unique();
        let ix = set_link_health(&pid, &payer, &link, LinkSetHealthArgs::default());
        assert_eq!(ix.data[0], 84);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(link, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }
}
