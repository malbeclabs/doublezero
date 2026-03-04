use crate::{
    error::{DoubleZeroError, Validate},
    format_option,
    helper::format_option_displayable,
    serializer::try_acc_write,
    state::{
        accounttype::AccountType,
        contributor::Contributor,
        device::*,
        globalstate::GlobalState,
        interface::{
            InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType, LoopbackType, RoutingMode,
        },
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
}

impl fmt::Debug for DeviceInterfaceUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "name: {}, loopback_type: {}, vlan_id: {}, user_tunnel_endpoint: {}, status: {}, \
ip_net: {}, node_segment_idx: {}, interface_cyoa: {}, interface_dia: {}, bandwidth: {}, \
cir: {}, mtu: {}, routing_mode: {}",
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
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_device_interface({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        device_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    // Check if the account is writable
    assert!(device_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    let (idx, _) = device
        .find_interface(&value.name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;
    let mut iface = device.interfaces[idx].into_current_version();

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
        iface.node_segment_idx = node_segment_idx;
    }

    // CYOA interfaces must have an ip_net — prevent setting CYOA without ip_net
    // or clearing ip_net from a CYOA interface via update
    if iface.interface_cyoa != InterfaceCYOA::None && iface.ip_net == NetworkV4::default() {
        return Err(DoubleZeroError::InvalidInterfaceIp.into());
    }

    // until we have release V2 version for interfaces, always convert to v1
    let updated_interface = iface.to_interface();

    updated_interface.validate()?;

    device.interfaces[idx] = updated_interface;

    try_acc_write(&device, device_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", device);

    Ok(())
}
