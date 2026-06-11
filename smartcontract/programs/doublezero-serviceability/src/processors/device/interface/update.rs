use crate::{
    error::{DoubleZeroError, Validate},
    format_option,
    helper::format_option_displayable,
    pda::get_resource_extension_pda,
    processors::{
        resource::{allocate_id, allocate_specific_id, deallocate_id},
        validation::validate_program_account,
    },
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        accounttype::AccountType,
        contributor::Contributor,
        device::*,
        globalstate::GlobalState,
        interface::{
            InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType, LoopbackType, RoutingMode,
            CYOA_DIA_INTERFACE_MTU, INTERFACE_MTU,
        },
        topology::FlexAlgoNodeSegment,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::collections::HashSet;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeviceInterfaceUpdateArgs {
    pub name: String,
    pub loopback_type: Option<LoopbackType>,
    pub vlan_id: Option<u16>,
    pub user_tunnel_endpoint: Option<bool>,
    pub status: Option<InterfaceStatus>,
    pub ip_net: Option<NetworkV4>,
    pub node_segment_idx: Option<u16>,
    pub interface_cyoa: Option<InterfaceCYOA>,
    pub interface_dia: Option<InterfaceDIA>,
    pub bandwidth: Option<u64>,
    pub cir: Option<u64>,
    pub mtu: Option<u16>,
    pub routing_mode: Option<RoutingMode>,
    /// Number of topology PDA accounts appended after segment_routing_ids.
    /// Only consumed when update_topologies is true.
    #[incremental(default = 0)]
    pub topology_count: u8,
    /// When true, the variadic topology accounts represent the desired set of
    /// flex-algo topologies; the processor reconciles flex_algo_node_segments
    /// (deallocate removed, allocate added). Only valid on Vpnv4 loopbacks.
    #[incremental(default = false)]
    pub update_topologies: bool,
}

impl fmt::Debug for DeviceInterfaceUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "name: {}, loopback_type: {}, vlan_id: {}, user_tunnel_endpoint: {}, status: {}, \
ip_net: {}, node_segment_idx: {}, interface_cyoa: {}, interface_dia: {}, bandwidth: {}, \
cir: {}, mtu: {}, routing_mode: {}, topology_count: {}, update_topologies: {}",
            self.name,
            format_option!(self.loopback_type),
            format_option!(self.vlan_id),
            format_option!(self.user_tunnel_endpoint),
            format_option!(self.status),
            format_option!(self.ip_net),
            format_option!(self.node_segment_idx),
            format_option!(self.interface_cyoa),
            format_option!(self.interface_dia),
            format_option!(self.bandwidth),
            format_option!(self.cir),
            format_option!(self.mtu),
            format_option!(self.routing_mode),
            self.topology_count,
            self.update_topologies,
        )
    }
}

pub fn process_update_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: SegmentRoutingIds resource extension account, present when
    // node_segment_idx is being updated under onchain allocation, OR when
    // reconciling flex-algo topologies.
    // Account layout when reconciling topologies:
    //   [device, contributor, globalstate, segment_routing_ids_ext, topo_0..N, payer, system]
    // Account layout for node_segment_idx under onchain allocation:
    //   [device, contributor, globalstate, segment_routing_ids_ext, payer, system]
    // Account layout WITHOUT (legacy):
    //   [device, contributor, globalstate, payer, system]
    //
    // The presence of update_topologies forces seg_ext consumption; otherwise
    // fall back to the legacy account-count heuristic so callers that set
    // node_segment_idx without onchain allocation enabled still work.
    let segment_routing_ids_ext = if value.update_topologies || accounts.len() > 5 {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    let mut topology_accounts = Vec::new();
    if value.update_topologies {
        for _ in 0..value.topology_count {
            topology_accounts.push(next_account_info(accounts_iter)?);
        }
    }

    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_device_interface({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(device_account, program_id, writable = true, "Device");
    validate_program_account!(
        contributor_account,
        program_id,
        writable = false,
        "Contributor"
    );
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        "GlobalState"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    // The supplied contributor must be the one the device belongs to,
    // unless the payer is on the foundation allowlist.
    if !globalstate.foundation_allowlist.contains(payer_account.key)
        && device.contributor_pk != *contributor_account.key
    {
        return Err(DoubleZeroError::InvalidContributorPubkey.into());
    }

    let (idx, _) = device
        .find_interface(&value.name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;
    let mut iface = device.interfaces[idx].clone();

    if let Some(loopback_type) = &value.loopback_type {
        if *loopback_type == LoopbackType::None {
            return Err(DoubleZeroError::InvalidLoopbackType.into());
        }
        iface.loopback_type = *loopback_type;
    }
    if let Some(interface_cyoa) = &value.interface_cyoa {
        if *interface_cyoa != InterfaceCYOA::None
            && iface.status == InterfaceStatus::Activated
            && iface.interface_type == InterfaceType::Physical
        {
            return Err(DoubleZeroError::InterfaceHasEdgeAssignment.into());
        }
        iface.interface_cyoa = *interface_cyoa;
    }
    if let Some(interface_dia) = &value.interface_dia {
        if *interface_dia != InterfaceDIA::None
            && iface.status == InterfaceStatus::Activated
            && iface.interface_type == InterfaceType::Physical
        {
            return Err(DoubleZeroError::InterfaceHasEdgeAssignment.into());
        }
        iface.interface_dia = *interface_dia;
    }
    if let Some(bandwidth) = value.bandwidth {
        iface.bandwidth = bandwidth;
    }
    if let Some(cir) = value.cir {
        iface.cir = cir;
    }
    if let Some(mtu) = value.mtu {
        iface.mtu = mtu;
    }
    if let Some(routing_mode) = value.routing_mode {
        iface.routing_mode = routing_mode;
    }
    if let Some(vlan_id) = value.vlan_id {
        iface.vlan_id = vlan_id;
    }
    if let Some(user_tunnel_endpoint) = value.user_tunnel_endpoint {
        iface.user_tunnel_endpoint = user_tunnel_endpoint;
    }
    if let Some(status) = value.status {
        iface.status = status;
    }
    if let Some(ip_net) = value.ip_net {
        // ip_net can only be set on CYOA, DIA, or user-tunnel-endpoint interfaces
        if iface.interface_cyoa == InterfaceCYOA::None
            && iface.interface_dia == InterfaceDIA::None
            && !iface.user_tunnel_endpoint
        {
            return Err(DoubleZeroError::InvalidInterfaceIp.into());
        }
        iface.ip_net = ip_net;
    }
    if let Some(node_segment_idx) = value.node_segment_idx {
        if !globalstate.foundation_allowlist.contains(payer_account.key) {
            return Err(DoubleZeroError::NotAllowed.into());
        }

        let seg_ext = segment_routing_ids_ext.ok_or(DoubleZeroError::InvalidArgument)?;

        let (expected_seg_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
        validate_program_account!(
            seg_ext,
            program_id,
            writable = true,
            pda = &expected_seg_pda,
            "SegmentRoutingIds"
        );

        // Deallocate old value if non-zero
        if iface.node_segment_idx != 0 {
            deallocate_id(seg_ext, iface.node_segment_idx);
        }

        // Allocate new value if non-zero
        if node_segment_idx != 0 {
            allocate_specific_id(seg_ext, node_segment_idx)?;
        }

        iface.node_segment_idx = node_segment_idx;
    }

    // Reconcile flex-algo node segments against the desired topology set.
    // Existing entries whose topology is still in the set keep their SR ID; entries
    // for removed topologies have their SR ID deallocated; new topologies get a
    // freshly allocated SR ID.
    if value.update_topologies {
        if contributor.owner != *payer_account.key
            && !globalstate.foundation_allowlist.contains(payer_account.key)
        {
            return Err(DoubleZeroError::NotAllowed.into());
        }

        if iface.loopback_type != LoopbackType::Vpnv4 {
            return Err(DoubleZeroError::InvalidArgument.into());
        }

        let seg_ext = segment_routing_ids_ext.ok_or(DoubleZeroError::InvalidArgument)?;

        let (expected_seg_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
        validate_program_account!(
            seg_ext,
            program_id,
            writable = true,
            pda = &expected_seg_pda,
            "SegmentRoutingIds"
        );

        let mut desired: HashSet<Pubkey> = HashSet::new();
        for topo_account in &topology_accounts {
            assert_eq!(
                topo_account.owner, program_id,
                "Invalid Topology Account Owner"
            );
            assert!(!topo_account.data_is_empty(), "Topology account is empty");
            let topo_type = AccountType::from(topo_account.try_borrow_data()?[0]);
            assert_eq!(
                topo_type,
                AccountType::Topology,
                "Invalid Topology Account Type"
            );
            if !desired.insert(*topo_account.key) {
                return Err(DoubleZeroError::InvalidArgument.into());
            }
        }

        let mut kept: Vec<FlexAlgoNodeSegment> =
            Vec::with_capacity(iface.flex_algo_node_segments.len().max(desired.len()));
        for entry in iface.flex_algo_node_segments.drain(..) {
            if desired.contains(&entry.topology) {
                kept.push(entry);
            } else {
                deallocate_id(seg_ext, entry.node_segment_idx);
            }
        }
        let current: HashSet<Pubkey> = kept.iter().map(|e| e.topology).collect();
        for topology in &desired {
            if !current.contains(topology) {
                let node_segment_idx = allocate_id(seg_ext)?;
                kept.push(FlexAlgoNodeSegment {
                    topology: *topology,
                    node_segment_idx,
                });
            }
        }
        iface.flex_algo_node_segments = kept;
    }

    // CYOA interfaces must have an ip_net — prevent setting CYOA without ip_net
    // or clearing ip_net from a CYOA interface via update
    if iface.interface_cyoa != InterfaceCYOA::None && iface.ip_net == NetworkV4::default() {
        return Err(DoubleZeroError::InvalidInterfaceIp.into());
    }

    // Validate MTU against the resulting CYOA/DIA state after all updates
    let is_cyoa_or_dia =
        iface.interface_cyoa != InterfaceCYOA::None || iface.interface_dia != InterfaceDIA::None;
    let expected_mtu = if is_cyoa_or_dia {
        CYOA_DIA_INTERFACE_MTU
    } else {
        INTERFACE_MTU
    };
    if iface.mtu != expected_mtu {
        return Err(DoubleZeroError::InvalidMtu.into());
    }

    // CYOA/DIA interfaces must have a non-zero bandwidth. Only enforce when the
    // transaction is changing CYOA, DIA, or bandwidth, so legacy zero-bandwidth
    // CYOA/DIA interfaces created before this rule can still be updated for
    // unrelated fields without first being repaired.
    let touches_bw_or_edge = value.interface_cyoa.is_some()
        || value.interface_dia.is_some()
        || value.bandwidth.is_some();
    if touches_bw_or_edge && is_cyoa_or_dia && iface.bandwidth == 0 {
        return Err(DoubleZeroError::InvalidBandwidth.into());
    }

    iface.validate()?;

    device.replace_interface(idx, iface);

    try_acc_write(&device, device_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", device);

    Ok(())
}
