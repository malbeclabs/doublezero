use crate::{
    error::DoubleZeroError,
    pda::get_link_pda,
    seeds::{SEED_LINK, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType,
        contributor::Contributor,
        device::Device,
        globalstate::GlobalState,
        interface::{InterfaceCYOA, InterfaceDIA, InterfaceStatus},
        link::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::{types::NetworkV4, validate_account_code};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

use super::resource_onchain_helpers;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkCreateArgs {
    pub code: String,
    pub link_type: LinkLinkType,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay_ns: u64,
    pub jitter_ns: u64,
    pub side_a_iface_name: String,
    pub side_z_iface_name: Option<String>,
    pub desired_status: Option<LinkDesiredStatus>,
    /// When true, on-chain allocation is used (ResourceExtension accounts required).
    /// Performs atomic create+allocate+activate in a single transaction.
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
}

impl fmt::Debug for LinkCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, link_type: {:?}, bandwidth: {}, mtu: {}, delay_ns: {}, jitter_ns: {}, side_a_iface_name: {}, side_z_iface_name: {:?}, desired_status: {:?}",
            self.code, self.link_type, self.bandwidth, self.mtu, self.delay_ns, self.jitter_ns, self.side_a_iface_name, self.side_z_iface_name, self.desired_status
        )
    }
}

pub fn process_create_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let side_a_account = next_account_info(accounts_iter)?;
    let side_z_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension accounts for on-chain allocation (before payer)
    // Account layout WITH ResourceExtension (use_onchain_allocation = true):
    //   [link, contributor, side_a, side_z, globalstate, device_tunnel_block, link_ids, payer, system]
    // Account layout WITHOUT (legacy, use_onchain_allocation = false):
    //   [link, contributor, side_a, side_z, globalstate, payer, system]
    let resource_extension_accounts = if value.use_onchain_allocation {
        let device_tunnel_block_ext = next_account_info(accounts_iter)?;
        let link_ids_ext = next_account_info(accounts_iter)?;
        Some((device_tunnel_block_ext, link_ids_ext))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_link({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        side_a_account.owner, program_id,
        "Invalid Side A Account Owner"
    );
    assert_eq!(
        side_z_account.owner, program_id,
        "Invalid Side Z Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    if !link_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    globalstate.account_index += 1;

    let mut contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    let (expected_pda_account, bump_seed) = get_link_pda(program_id, globalstate.account_index);
    assert_eq!(
        link_account.key, &expected_pda_account,
        "Invalid Link PubKey"
    );

    let mut side_a_dev = Device::try_from(side_a_account)?;

    if side_a_dev.contributor_pk != *contributor_account.key {
        return Err(DoubleZeroError::InvalidContributor.into());
    }

    let mut side_z_dev = Device::try_from(side_z_account)?;

    if value.link_type != LinkLinkType::DZX && side_z_dev.contributor_pk != *contributor_account.key
    {
        return Err(DoubleZeroError::InvalidContributor.into());
    }

    let side_a_iface = side_a_dev
        .interfaces
        .iter()
        .map(|iface| iface.into_current_version())
        .find(|iface| iface.name == value.side_a_iface_name)
        .ok_or_else(|| {
            #[cfg(test)]
            msg!("{:?}", side_a_dev);
            ProgramError::from(DoubleZeroError::InvalidInterfaceName)
        })?;

    if side_a_iface.interface_cyoa != InterfaceCYOA::None
        || side_a_iface.interface_dia != InterfaceDIA::None
    {
        return Err(DoubleZeroError::InterfaceHasEdgeAssignment.into());
    }

    let side_z_iface_name = value.side_z_iface_name.clone().unwrap_or_default();
    if let Some(ref z_name) = value.side_z_iface_name {
        let side_z_iface = side_z_dev
            .interfaces
            .iter()
            .map(|iface| iface.into_current_version())
            .find(|iface| iface.name == *z_name)
            .ok_or_else(|| {
                #[cfg(test)]
                msg!("{:?}", side_z_dev);
                ProgramError::from(DoubleZeroError::InvalidInterfaceName)
            })?;

        if side_z_iface.interface_cyoa != InterfaceCYOA::None
            || side_z_iface.interface_dia != InterfaceDIA::None
        {
            return Err(DoubleZeroError::InterfaceHasEdgeAssignment.into());
        }
    }
    if value.link_type == LinkLinkType::DZX && value.side_z_iface_name.is_some() {
        return Err(DoubleZeroError::InvalidInterfaceZForExternal.into());
    }

    let status = if value.link_type == LinkLinkType::DZX {
        LinkStatus::Requested
    } else {
        LinkStatus::Pending
    };

    contributor.reference_count += 1;
    side_a_dev.reference_count += 1;
    side_z_dev.reference_count += 1;

    let mut link = Link {
        account_type: AccountType::Link,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        code,
        contributor_pk: *contributor_account.key,
        side_a_pk: *side_a_account.key,
        side_z_pk: *side_z_account.key,
        link_type: value.link_type,
        bandwidth: value.bandwidth,
        mtu: value.mtu,
        delay_ns: value.delay_ns,
        jitter_ns: value.jitter_ns,
        tunnel_id: 0,
        tunnel_net: NetworkV4::default(),
        status,
        side_a_iface_name: value.side_a_iface_name.clone(),
        side_z_iface_name,
        delay_override_ns: 0,
        // TODO: This line show be change when the health oracle is implemented
        // link_health: LinkHealth::Pending,
        link_health: LinkHealth::ReadyForService, // Force the link to be ready for service until the health oracle is implemented,
        desired_status: value.desired_status.unwrap_or(LinkDesiredStatus::Activated),
    };

    link.check_status_transition();

    // Atomic create+allocate+activate if on-chain allocation is enabled
    if let Some((device_tunnel_block_ext, link_ids_ext)) = resource_extension_accounts {
        let globalstate_ref = GlobalState::try_from(globalstate_account)?;
        resource_onchain_helpers::validate_and_allocate_link_resources(
            program_id,
            &mut link,
            device_tunnel_block_ext,
            link_ids_ext,
            &globalstate_ref,
        )?;

        // Validate interfaces are Unlinked (required for activation)
        let (idx_a, side_a_iface) = side_a_dev
            .find_interface(&link.side_a_iface_name)
            .map_err(|_| DoubleZeroError::InterfaceNotFound)?;
        if side_a_iface.status != InterfaceStatus::Unlinked {
            return Err(DoubleZeroError::InvalidStatus.into());
        }

        // Set side A interface to Activated with IP from tunnel_net
        let mut updated_iface_a = side_a_iface.clone();
        updated_iface_a.status = InterfaceStatus::Activated;
        if updated_iface_a.ip_net == NetworkV4::default() {
            updated_iface_a.ip_net =
                NetworkV4::new(link.tunnel_net.nth(0).unwrap(), link.tunnel_net.prefix()).unwrap();
        }
        side_a_dev.interfaces[idx_a] = updated_iface_a.to_interface();

        // Set side Z interface to Activated with IP from tunnel_net
        if let Ok((idx_z, side_z_iface)) = side_z_dev.find_interface(&link.side_z_iface_name) {
            if side_z_iface.status != InterfaceStatus::Unlinked {
                return Err(DoubleZeroError::InvalidStatus.into());
            }
            let mut updated_iface_z = side_z_iface.clone();
            updated_iface_z.status = InterfaceStatus::Activated;
            if updated_iface_z.ip_net == NetworkV4::default() {
                updated_iface_z.ip_net =
                    NetworkV4::new(link.tunnel_net.nth(1).unwrap(), link.tunnel_net.prefix())
                        .unwrap();
            }
            side_z_dev.interfaces[idx_z] = updated_iface_z.to_interface();
        }

        link.status = LinkStatus::Activated;
        link.check_status_transition();
    }

    try_acc_create(
        &link,
        link_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_LINK,
            &globalstate.account_index.to_le_bytes(),
            &[bump_seed],
        ],
    )?;
    try_acc_write(&contributor, contributor_account, payer_account, accounts)?;
    try_acc_write(&side_a_dev, side_a_account, payer_account, accounts)?;
    try_acc_write(&side_z_dev, side_z_account, payer_account, accounts)?;
    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    Ok(())
}
