use crate::{
    error::{DoubleZeroError, Validate},
    pda::get_resource_extension_pda,
    processors::{
        resource::{allocate_specific_id, allocate_specific_ip, deallocate_id, deallocate_ip},
        validation::validate_program_account,
    },
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        contributor::Contributor,
        device::Device,
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        link::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::{types::NetworkV4, validate_account_code};

use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkUpdateArgs {
    pub code: Option<String>,
    pub contributor_pk: Option<Pubkey>,
    pub tunnel_type: Option<LinkLinkType>,
    pub bandwidth: Option<u64>,
    pub mtu: Option<u32>,
    pub delay_ns: Option<u64>,
    pub jitter_ns: Option<u64>,
    pub status: Option<LinkStatus>,
    pub delay_override_ns: Option<u64>,
    pub desired_status: Option<LinkDesiredStatus>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
    pub link_topologies: Option<Vec<Pubkey>>,
    #[incremental(default = None)]
    pub unicast_drained: Option<bool>,
}

impl fmt::Debug for LinkUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let mut parts = Vec::new();
        if let Some(ref code) = self.code {
            parts.push(format!("code: {:?}", code));
        }
        if let Some(ref contributor_pk) = self.contributor_pk {
            parts.push(format!("contributor_pk: {:?}", contributor_pk));
        }
        if let Some(ref tunnel_type) = self.tunnel_type {
            parts.push(format!("tunnel_type: {:?}", tunnel_type));
        }
        if let Some(bandwidth) = self.bandwidth {
            parts.push(format!("bandwidth: {:?}", bandwidth));
        }
        if let Some(mtu) = self.mtu {
            parts.push(format!("mtu: {:?}", mtu));
        }
        if let Some(delay_ns) = self.delay_ns {
            parts.push(format!("delay_ns: {:?}", delay_ns));
        }
        if let Some(jitter_ns) = self.jitter_ns {
            parts.push(format!("jitter_ns: {:?}", jitter_ns));
        }
        if let Some(ref status) = self.status {
            parts.push(format!("status: {:?}", status));
        }
        if let Some(delay_override_ns) = self.delay_override_ns {
            parts.push(format!("delay_override_ns: {:?}", delay_override_ns));
        }
        if let Some(ref desired_status) = self.desired_status {
            parts.push(format!("desired_status: {:?}", desired_status));
        }
        if let Some(tunnel_id) = self.tunnel_id {
            parts.push(format!("tunnel_id: {:?}", tunnel_id));
        }
        if let Some(ref tunnel_net) = self.tunnel_net {
            parts.push(format!("tunnel_net: {:?}", tunnel_net));
        }
        if self.use_onchain_allocation {
            parts.push("use_onchain_allocation: true".to_string());
        }
        if let Some(ref link_topologies) = self.link_topologies {
            parts.push(format!("link_topologies: {:?}", link_topologies));
        }
        if let Some(unicast_drained) = self.unicast_drained {
            parts.push(format!("unicast_drained: {:?}", unicast_drained));
        }
        write!(f, "{}", parts.join(", "))
    }
}

pub fn process_update_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;

    // Account layout (all optional accounts included):
    //   [link, contributor, side_z?, globalstate, device_a?, device_z?, device_tunnel_block?, link_ids?, payer, system]
    // device_a/device_z: present when tunnel_net is being updated (needed for interface IP update)
    // device_tunnel_block/link_ids: present when use_onchain_allocation is true
    let mut expected_without_side_z: usize = 5; // link, contributor, globalstate, payer, system
    if value.tunnel_net.is_some() {
        expected_without_side_z += 2; // device_a, device_z
    }
    if value.use_onchain_allocation {
        expected_without_side_z += 2; // device_tunnel_block, link_ids
    }
    let side_z_account: Option<&AccountInfo> = if accounts.len() > expected_without_side_z {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };
    let globalstate_account = next_account_info(accounts_iter)?;

    let device_accounts = if value.tunnel_net.is_some() {
        let device_a_account = next_account_info(accounts_iter)?;
        let device_z_account = next_account_info(accounts_iter)?;
        Some((device_a_account, device_z_account))
    } else {
        None
    };

    let resource_accounts = if value.use_onchain_allocation {
        let device_tunnel_block_ext = next_account_info(accounts_iter)?;
        let link_ids_ext = next_account_info(accounts_iter)?;
        Some((device_tunnel_block_ext, link_ids_ext))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_link({:?})", value);

    // Check if the payer is a signer
    assert!(
        payer_account.is_signer,
        "Payer must be a signer {:?}",
        payer_account
    );

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(link_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    let contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        msg!("contributor owner: {:?}", contributor.owner);
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if let Some(side_z_account) = side_z_account {
        if side_z_account.owner != program_id {
            return Err(DoubleZeroError::InvalidAccountOwner.into());
        }
    }

    // Deserialize the optional side_z device account
    let side_z: Option<Device> = if let Some(side_z_account) = side_z_account {
        Some(Device::try_from(side_z_account)?)
    } else {
        None
    };

    // Deserialize the link account
    let mut link: Link = Link::try_from(link_account)?;

    if side_z.is_none() {
        // Link should be owned by the contributor A
        if link.contributor_pk != *contributor_account.key {
            msg!("link contributor_pk: {:?}", link.contributor_pk);
            return Err(DoubleZeroError::NotAllowed.into());
        }
    } else if let Some(side_z) = side_z {
        // Link should be owned by the side_z device's contributor B
        if link.side_z_pk != *side_z_account.unwrap().key {
            return Err(DoubleZeroError::InvalidAccountOwner.into());
        }
        if side_z.contributor_pk != *contributor_account.key {
            msg!("side_z contributor_pk: {:?}", side_z.contributor_pk);
            return Err(DoubleZeroError::NotAllowed.into());
        }
    }

    // can be updated by contributor A
    if link.contributor_pk == *contributor_account.key {
        if let Some(ref code) = value.code {
            let mut code =
                validate_account_code(code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
            code.make_ascii_lowercase();
            link.code = code;
        }
        if let Some(tunnel_type) = value.tunnel_type {
            link.link_type = tunnel_type;
        }
        if let Some(bandwidth) = value.bandwidth {
            link.bandwidth = bandwidth;
        }
        if let Some(mtu) = value.mtu {
            link.mtu = mtu;
        }
        if let Some(delay_ns) = value.delay_ns {
            link.delay_ns = delay_ns;
        }
        if let Some(jitter_ns) = value.jitter_ns {
            link.jitter_ns = jitter_ns;
        }
    }
    // Can be updated by both contributors A and B
    if let Some(delay_override_ns) = value.delay_override_ns {
        link.delay_override_ns = delay_override_ns;
    }
    if let Some(desired_status) = value.desired_status {
        link.desired_status = desired_status;
    }

    if let Some(status) = value.status {
        link.status = status;
    }

    // Handle tunnel_id/tunnel_net reallocation (foundation-only)
    if value.tunnel_id.is_some() || value.tunnel_net.is_some() {
        if !globalstate.foundation_allowlist.contains(payer_account.key) {
            msg!("tunnel field updates require foundation allowlist");
            return Err(DoubleZeroError::NotAllowed.into());
        }

        if let Some((device_tunnel_block_ext, link_ids_ext)) = resource_accounts {
            // Resource accounts provided — require feature flag
            if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
                return Err(DoubleZeroError::FeatureNotEnabled.into());
            }

            // Validate DeviceTunnelBlock PDA
            let (expected_device_tunnel_pda, _, _) =
                get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
            validate_program_account!(
                device_tunnel_block_ext,
                program_id,
                writable = true,
                pda = Some(&expected_device_tunnel_pda),
                "DeviceTunnelBlock"
            );

            // Validate LinkIds PDA
            let (expected_link_ids_pda, _, _) =
                get_resource_extension_pda(program_id, ResourceType::LinkIds);
            validate_program_account!(
                link_ids_ext,
                program_id,
                writable = true,
                pda = Some(&expected_link_ids_pda),
                "LinkIds"
            );

            // Deallocate/allocate tunnel_id
            if let Some(new_tunnel_id) = value.tunnel_id {
                if link.tunnel_id != 0 {
                    deallocate_id(link_ids_ext, link.tunnel_id);
                    #[cfg(test)]
                    msg!("Deallocated old tunnel_id {}", link.tunnel_id);
                }
                if new_tunnel_id != 0 {
                    allocate_specific_id(link_ids_ext, new_tunnel_id)?;
                    #[cfg(test)]
                    msg!("Allocated new tunnel_id {}", new_tunnel_id);
                }
                link.tunnel_id = new_tunnel_id;
            }

            // Deallocate/allocate tunnel_net
            if let Some(new_tunnel_net) = value.tunnel_net {
                if link.tunnel_net != NetworkV4::default() {
                    deallocate_ip(device_tunnel_block_ext, link.tunnel_net);
                    #[cfg(test)]
                    msg!("Deallocated old tunnel_net {}", link.tunnel_net);
                }
                if new_tunnel_net != NetworkV4::default() {
                    allocate_specific_ip(device_tunnel_block_ext, new_tunnel_net)?;
                    #[cfg(test)]
                    msg!("Allocated new tunnel_net {}", new_tunnel_net);
                }
                link.tunnel_net = new_tunnel_net;
            }
        } else {
            // Legacy path: no resource accounts, just overwrite fields
            if let Some(tunnel_id) = value.tunnel_id {
                link.tunnel_id = tunnel_id;
            }
            if let Some(tunnel_net) = value.tunnel_net {
                link.tunnel_net = tunnel_net;
            }
        }
    }

    // Update device interface IPs when tunnel_net was changed
    if let Some((device_a_account, device_z_account)) = device_accounts {
        assert_eq!(
            device_a_account.owner, program_id,
            "Invalid Device A Account Owner"
        );
        assert_eq!(
            device_z_account.owner, program_id,
            "Invalid Device Z Account Owner"
        );
        assert!(
            device_a_account.is_writable,
            "Device A Account is not writable"
        );
        assert!(
            device_z_account.is_writable,
            "Device Z Account is not writable"
        );

        if link.side_a_pk != *device_a_account.key || link.side_z_pk != *device_z_account.key {
            return Err(ProgramError::InvalidAccountData);
        }

        let mut side_a_dev = Device::try_from(device_a_account)?;
        let mut side_z_dev = Device::try_from(device_z_account)?;

        let (idx_a, side_a_iface) = side_a_dev
            .find_interface(&link.side_a_iface_name)
            .map_err(|_| DoubleZeroError::InterfaceNotFound)?;
        let mut updated_iface_a = side_a_iface.clone();
        updated_iface_a.ip_net =
            NetworkV4::new(link.tunnel_net.nth(0).unwrap(), link.tunnel_net.prefix()).unwrap();
        side_a_dev.interfaces[idx_a] = updated_iface_a.to_interface();

        let (idx_z, side_z_iface) = side_z_dev
            .find_interface(&link.side_z_iface_name)
            .map_err(|_| DoubleZeroError::InterfaceNotFound)?;
        let mut updated_iface_z = side_z_iface.clone();
        updated_iface_z.ip_net =
            NetworkV4::new(link.tunnel_net.nth(1).unwrap(), link.tunnel_net.prefix()).unwrap();
        side_z_dev.interfaces[idx_z] = updated_iface_z.to_interface();

        try_acc_write(&side_a_dev, device_a_account, payer_account, accounts)?;
        try_acc_write(&side_z_dev, device_z_account, payer_account, accounts)?;
    }

    // link_topologies is foundation-only
    if let Some(link_topologies) = &value.link_topologies {
        if !globalstate.foundation_allowlist.contains(payer_account.key) {
            msg!("link_topologies update requires foundation allowlist");
            return Err(DoubleZeroError::NotAllowed.into());
        }
        link.link_topologies = link_topologies.clone();
    }

    // unicast_drained (LINK_FLAG_UNICAST_DRAINED bit 0): contributor A or foundation
    if let Some(unicast_drained) = value.unicast_drained {
        if link.contributor_pk != *contributor_account.key
            && !globalstate.foundation_allowlist.contains(payer_account.key)
        {
            msg!("unicast_drained update requires contributor A or foundation allowlist");
            return Err(DoubleZeroError::NotAllowed.into());
        }
        if unicast_drained {
            link.link_flags |= crate::state::link::LINK_FLAG_UNICAST_DRAINED;
        } else {
            link.link_flags &= !crate::state::link::LINK_FLAG_UNICAST_DRAINED;
        }
    }

    link.check_status_transition();
    link.validate()?;

    try_acc_write(&link, link_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", link);

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_deserialize_link_update_args_before_delay_override_ns() {
        #[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default, Debug)]
        pub struct LinkUpdateArgsBeforeDelayOverrideNs {
            pub code: Option<String>,
            pub contributor_pk: Option<Pubkey>,
            pub tunnel_type: Option<LinkLinkType>,
            pub bandwidth: Option<u64>,
            pub mtu: Option<u32>,
            pub delay_ns: Option<u64>,
            pub jitter_ns: Option<u64>,
            pub status: Option<LinkStatus>,
        }

        let contributor_pk = Pubkey::new_unique();

        let args_before = LinkUpdateArgsBeforeDelayOverrideNs {
            code: Some("test-code".to_string()),
            contributor_pk: Some(contributor_pk),
            tunnel_type: Some(LinkLinkType::WAN),
            bandwidth: Some(10_000_000_000),
            mtu: Some(1500),
            delay_ns: Some(1_000_000),
            jitter_ns: Some(100_000),
            status: Some(LinkStatus::Activated),
        };

        let expected = LinkUpdateArgs {
            code: Some("test-code".to_string()),
            contributor_pk: Some(contributor_pk),
            tunnel_type: Some(LinkLinkType::WAN),
            bandwidth: Some(10_000_000_000),
            mtu: Some(1500),
            delay_ns: Some(1_000_000),
            jitter_ns: Some(100_000),
            status: Some(LinkStatus::Activated),
            delay_override_ns: None,
            desired_status: None,
            tunnel_id: None,
            tunnel_net: None,
            use_onchain_allocation: false,
            link_topologies: None,
            unicast_drained: None,
        };

        let serialized = borsh::to_vec(&args_before).unwrap();
        let deserialized = LinkUpdateArgs::try_from(&serialized[..]).unwrap();

        assert_eq!(expected, deserialized);
    }

    #[test]
    fn test_deserialize_link_update_args_before_tunnel_fields() {
        #[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default, Debug)]
        pub struct LinkUpdateArgsBeforeTunnelFields {
            pub code: Option<String>,
            pub contributor_pk: Option<Pubkey>,
            pub tunnel_type: Option<LinkLinkType>,
            pub bandwidth: Option<u64>,
            pub mtu: Option<u32>,
            pub delay_ns: Option<u64>,
            pub jitter_ns: Option<u64>,
            pub status: Option<LinkStatus>,
            pub delay_override_ns: Option<u64>,
            pub desired_status: Option<LinkDesiredStatus>,
        }

        let contributor_pk = Pubkey::new_unique();

        let args_before = LinkUpdateArgsBeforeTunnelFields {
            code: Some("test-code".to_string()),
            contributor_pk: Some(contributor_pk),
            tunnel_type: Some(LinkLinkType::WAN),
            bandwidth: Some(10_000_000_000),
            mtu: Some(1500),
            delay_ns: Some(1_000_000),
            jitter_ns: Some(100_000),
            status: Some(LinkStatus::Activated),
            delay_override_ns: Some(500_000),
            desired_status: Some(LinkDesiredStatus::Activated),
        };

        let expected = LinkUpdateArgs {
            code: Some("test-code".to_string()),
            contributor_pk: Some(contributor_pk),
            tunnel_type: Some(LinkLinkType::WAN),
            bandwidth: Some(10_000_000_000),
            mtu: Some(1500),
            delay_ns: Some(1_000_000),
            jitter_ns: Some(100_000),
            status: Some(LinkStatus::Activated),
            delay_override_ns: Some(500_000),
            desired_status: Some(LinkDesiredStatus::Activated),
            tunnel_id: None,
            tunnel_net: None,
            use_onchain_allocation: false,
            link_topologies: None,
            unicast_drained: None,
        };

        let serialized = borsh::to_vec(&args_before).unwrap();
        let deserialized = LinkUpdateArgs::try_from(&serialized[..]).unwrap();

        assert_eq!(expected, deserialized);
    }
}
