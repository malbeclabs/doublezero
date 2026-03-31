use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::{
        resource::{allocate_id, allocate_ip},
        validation::validate_program_account,
    },
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        accounttype::AccountType,
        contributor::Contributor,
        device::*,
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        interface::{
            CurrentInterfaceVersion, InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType,
            LoopbackType, RoutingMode,
        },
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::{types::NetworkV4, validate_iface};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeviceInterfaceCreateArgs {
    pub name: String,
    pub loopback_type: LoopbackType,
    pub vlan_id: u16,
    pub ip_net: Option<NetworkV4>,
    pub user_tunnel_endpoint: bool,
    pub interface_cyoa: InterfaceCYOA,
    pub interface_dia: InterfaceDIA,
    pub bandwidth: u64,
    pub cir: u64,
    pub mtu: u16,
    pub routing_mode: RoutingMode,
    /// When true, onchain allocation is used (ResourceExtension accounts required).
    /// Performs atomic create+allocate+activate in a single transaction.
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
}

impl fmt::Debug for DeviceInterfaceCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "name: {}, loopback_type: {}, vlan_id: {}, ip_net: {:?}, user_tunnel_endpoint: {}, \
interface_cyoa: {:?}, interface_dia: {:?}, bandwidth: {}, cir: {}, mtu: {}, routing_mode: {:?}, \
use_onchain_allocation: {}",
            self.name,
            self.loopback_type,
            self.vlan_id,
            self.ip_net,
            self.user_tunnel_endpoint,
            self.interface_cyoa,
            self.interface_dia,
            self.bandwidth,
            self.cir,
            self.mtu,
            self.routing_mode,
            self.use_onchain_allocation,
        )
    }
}

pub fn process_create_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension accounts for onchain allocation (before payer)
    // Account layout WITH ResourceExtension (use_onchain_allocation = true):
    //   [device, contributor, globalstate, device_tunnel_block, segment_routing_ids, payer, system]
    // Account layout WITHOUT (legacy, use_onchain_allocation = false):
    //   [device, contributor, globalstate, payer, system]
    let resource_accounts = if value.use_onchain_allocation {
        let device_tunnel_block_ext = next_account_info(accounts_iter)?;
        let segment_routing_ids_ext = next_account_info(accounts_iter)?;
        Some((device_tunnel_block_ext, segment_routing_ids_ext))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_device_interface({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    let name = validate_iface(&value.name).map_err(|_| DoubleZeroError::InvalidInterfaceName)?;

    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );

    assert!(device_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    let mut interface_type = InterfaceType::Physical;
    if name.starts_with("Loopback") {
        interface_type = InterfaceType::Loopback;
    }

    // CYOA can only be set on physical interfaces
    if value.interface_cyoa != InterfaceCYOA::None && interface_type != InterfaceType::Physical {
        return Err(DoubleZeroError::CyoaRequiresPhysical.into());
    }

    // ip_net can only be set on CYOA, DIA, or user-tunnel-endpoint interfaces
    if value.ip_net.is_some()
        && value.interface_cyoa == InterfaceCYOA::None
        && value.interface_dia == InterfaceDIA::None
        && !value.user_tunnel_endpoint
    {
        return Err(DoubleZeroError::InvalidInterfaceIp.into());
    }

    // CYOA interfaces must have an ip_net
    if value.interface_cyoa != InterfaceCYOA::None && value.ip_net.is_none() {
        return Err(DoubleZeroError::InvalidInterfaceIp.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    if device.find_interface(&name).is_ok() {
        return Err(DoubleZeroError::InterfaceAlreadyExists.into());
    }

    let mut status = InterfaceStatus::Pending;
    let mut ip_net = value.ip_net.unwrap_or_default();
    let mut node_segment_idx: u16 = 0;

    // Atomic create+allocate+activate if onchain allocation is enabled
    if let Some((device_tunnel_block_ext, segment_routing_ids_ext)) = resource_accounts {
        if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
            return Err(DoubleZeroError::FeatureNotEnabled.into());
        }

        let (expected_dtb_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
        validate_program_account!(
            device_tunnel_block_ext,
            program_id,
            writable = true,
            pda = Some(&expected_dtb_pda),
            "DeviceTunnelBlock"
        );

        let (expected_sr_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
        validate_program_account!(
            segment_routing_ids_ext,
            program_id,
            writable = true,
            pda = Some(&expected_sr_pda),
            "SegmentRoutingIds"
        );

        if interface_type == InterfaceType::Loopback {
            // Allocate IP from DeviceTunnelBlock
            ip_net = allocate_ip(device_tunnel_block_ext, 1)?;

            // Allocate segment routing ID for Vpnv4 loopbacks
            if value.loopback_type == LoopbackType::Vpnv4 {
                node_segment_idx = allocate_id(segment_routing_ids_ext)?;
            }

            status = InterfaceStatus::Activated;
        } else {
            // Physical interfaces go directly to Unlinked
            status = InterfaceStatus::Unlinked;
        }
    }

    device.interfaces.push(
        CurrentInterfaceVersion {
            status,
            name,
            interface_type,
            loopback_type: value.loopback_type,
            interface_cyoa: value.interface_cyoa,
            interface_dia: value.interface_dia,
            bandwidth: value.bandwidth,
            cir: value.cir,
            mtu: value.mtu,
            routing_mode: value.routing_mode,
            vlan_id: value.vlan_id,
            ip_net,
            node_segment_idx,
            user_tunnel_endpoint: value.user_tunnel_endpoint,
            flex_algo_node_segments: vec![],
        }
        .to_interface(),
    );

    try_acc_write(&device, device_account, payer_account, accounts)?;

    Ok(())
}
