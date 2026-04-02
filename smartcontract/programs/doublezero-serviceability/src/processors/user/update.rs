use crate::{
    error::DoubleZeroError,
    format_option,
    helper::format_option_displayable,
    pda::get_resource_extension_pda,
    processors::{
        resource::{allocate_specific_id, allocate_specific_ip, deallocate_id, deallocate_ip},
        validation::validate_program_account,
    },
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        tenant::Tenant,
        user::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct UserUpdateArgs {
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub dz_ip: Option<std::net::Ipv4Addr>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
    pub validator_pubkey: Option<Pubkey>,
    pub tenant_pk: Option<Pubkey>,
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
    #[incremental(default = 0)]
    pub multicast_publisher_count: u8,
}

impl fmt::Debug for UserUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, dz_ip: {}, tunnel_id: {}, tunnel_net: {}, validator_pubkey: {}, tenant_pk: {}, dz_prefix_count: {}, multicast_publisher_count: {}",
            format_option!(self.user_type),
            format_option!(self.cyoa_type),
            format_option!(self.dz_ip),
            format_option!(self.tunnel_id),
            format_option!(self.tunnel_net),
            format_option!(self.validator_pubkey),
            format_option!(self.tenant_pk),
            self.dz_prefix_count,
            self.multicast_publisher_count,
        )
    }
}

pub fn process_update_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Account layout WITH resource accounts (dz_prefix_count > 0):
    //   [user, globalstate, user_tunnel_block, multicast_publisher_block?, device_tunnel_ids, dz_prefix_0..N, old_tenant?, new_tenant?, payer, system]
    // Account layout WITHOUT (legacy, dz_prefix_count == 0):
    //   [user, globalstate, old_tenant?, new_tenant?, payer, system]
    let resource_accounts = if value.dz_prefix_count > 0 {
        let user_tunnel_block_ext = next_account_info(accounts_iter)?;

        let multicast_publisher_block_ext = if value.multicast_publisher_count > 0 {
            Some(next_account_info(accounts_iter)?)
        } else {
            None
        };

        let device_tunnel_ids_ext = next_account_info(accounts_iter)?;

        let mut dz_prefix_accounts = Vec::with_capacity(value.dz_prefix_count as usize);
        for _ in 0..value.dz_prefix_count {
            dz_prefix_accounts.push(next_account_info(accounts_iter)?);
        }

        Some((
            user_tunnel_block_ext,
            multicast_publisher_block_ext,
            device_tunnel_ids_ext,
            dz_prefix_accounts,
        ))
    } else {
        None
    };

    // Tenant accounts are optional — present when tenant_pk is being updated.
    // We compute the expected count of remaining accounts to detect their presence.
    // Remaining accounts: [old_tenant?, new_tenant?, payer, system]
    // With tenants: 4 remaining. Without tenants: 2 remaining.
    let remaining: Vec<_> = accounts_iter.collect();
    let has_tenant_accounts = remaining.len() >= 4;
    let (old_tenant_account, new_tenant_account, payer_account, _system_program) =
        if has_tenant_accounts {
            (
                Some(remaining[0]),
                Some(remaining[1]),
                remaining[2],
                remaining[3],
            )
        } else {
            (None, None, remaining[0], remaining[1])
        };

    #[cfg(test)]
    msg!("process_update_user({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(
        user_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "User"
    );
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "GlobalState"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::try_from(user_account)?;

    // Handle resource allocation/deallocation for tunnel_id, tunnel_net, dz_ip
    if let Some((
        user_tunnel_block_ext,
        multicast_publisher_block_ext,
        device_tunnel_ids_ext,
        ref dz_prefix_accounts,
    )) = resource_accounts
    {
        // Resource accounts provided — require feature flag
        if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
            return Err(DoubleZeroError::FeatureNotEnabled.into());
        }

        // Validate UserTunnelBlock PDA
        let (expected_user_tunnel_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
        validate_program_account!(
            user_tunnel_block_ext,
            program_id,
            writable = true,
            pda = Some(&expected_user_tunnel_pda),
            "UserTunnelBlock"
        );

        // Validate MulticastPublisherBlock PDA if provided
        if let Some(multicast_publisher_ext) = multicast_publisher_block_ext {
            let (expected_multicast_publisher_pda, _, _) =
                get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
            validate_program_account!(
                multicast_publisher_ext,
                program_id,
                writable = true,
                pda = Some(&expected_multicast_publisher_pda),
                "MulticastPublisherBlock"
            );
        }

        // Validate TunnelIds PDA
        let (expected_tunnel_ids_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::TunnelIds(user.device_pk, 0));
        validate_program_account!(
            device_tunnel_ids_ext,
            program_id,
            writable = true,
            pda = Some(&expected_tunnel_ids_pda),
            "TunnelIds"
        );

        // Validate all DzPrefixBlock accounts
        for (idx, dz_prefix_account) in dz_prefix_accounts.iter().enumerate() {
            let (expected_dz_prefix_pda, _, _) = get_resource_extension_pda(
                program_id,
                ResourceType::DzPrefixBlock(user.device_pk, idx),
            );
            validate_program_account!(
                dz_prefix_account,
                program_id,
                writable = true,
                pda = Some(&expected_dz_prefix_pda),
                &format!("DzPrefixBlock[{idx}]")
            );
        }

        // Deallocate/allocate tunnel_id
        if let Some(new_tunnel_id) = value.tunnel_id {
            if user.tunnel_id != 0 {
                deallocate_id(device_tunnel_ids_ext, user.tunnel_id);
                #[cfg(test)]
                msg!("Deallocated old tunnel_id {}", user.tunnel_id);
            }
            if new_tunnel_id != 0 {
                allocate_specific_id(device_tunnel_ids_ext, new_tunnel_id)?;
                #[cfg(test)]
                msg!("Allocated new tunnel_id {}", new_tunnel_id);
            }
            user.tunnel_id = new_tunnel_id;
        }

        // Deallocate/allocate tunnel_net
        if let Some(new_tunnel_net) = value.tunnel_net {
            if user.tunnel_net != NetworkV4::default() {
                deallocate_ip(user_tunnel_block_ext, user.tunnel_net);
                #[cfg(test)]
                msg!("Deallocated old tunnel_net {}", user.tunnel_net);
            }
            if new_tunnel_net != NetworkV4::default() {
                allocate_specific_ip(user_tunnel_block_ext, new_tunnel_net)?;
                #[cfg(test)]
                msg!("Allocated new tunnel_net {}", new_tunnel_net);
            }
            user.tunnel_net = new_tunnel_net;
        }

        // Deallocate/allocate dz_ip
        if let Some(new_dz_ip) = value.dz_ip {
            // Deallocate old dz_ip if it was allocated (not client_ip and not UNSPECIFIED)
            if user.dz_ip != user.client_ip && user.dz_ip != Ipv4Addr::UNSPECIFIED {
                if let Ok(old_dz_ip_net) = NetworkV4::new(user.dz_ip, 32) {
                    let mut deallocated = false;

                    // Try MulticastPublisherBlock first
                    if let Some(multicast_publisher_ext) = multicast_publisher_block_ext {
                        deallocated = deallocate_ip(multicast_publisher_ext, old_dz_ip_net);
                        #[cfg(test)]
                        msg!(
                            "Deallocated old dz_ip {} from MulticastPublisherBlock: {}",
                            old_dz_ip_net,
                            deallocated
                        );
                    }

                    // Fall back to DzPrefixBlock
                    if !deallocated {
                        for dz_prefix_account in dz_prefix_accounts.iter() {
                            deallocated = deallocate_ip(dz_prefix_account, old_dz_ip_net);
                            #[cfg(test)]
                            msg!(
                                "Deallocated old dz_ip {} from DzPrefixBlock: {}",
                                old_dz_ip_net,
                                deallocated
                            );
                            if deallocated {
                                break;
                            }
                        }
                    }
                }
            }

            // Allocate new dz_ip if it's not UNSPECIFIED and not client_ip
            if new_dz_ip != Ipv4Addr::UNSPECIFIED && new_dz_ip != user.client_ip {
                if let Ok(new_dz_ip_net) = NetworkV4::new(new_dz_ip, 32) {
                    // Try MulticastPublisherBlock first
                    let mut allocated = false;
                    if let Some(multicast_publisher_ext) = multicast_publisher_block_ext {
                        if allocate_specific_ip(multicast_publisher_ext, new_dz_ip_net).is_ok() {
                            allocated = true;
                            #[cfg(test)]
                            msg!(
                                "Allocated new dz_ip {} in MulticastPublisherBlock",
                                new_dz_ip_net
                            );
                        }
                    }

                    // Fall back to DzPrefixBlock
                    if !allocated {
                        let mut found = false;
                        for dz_prefix_account in dz_prefix_accounts.iter() {
                            if allocate_specific_ip(dz_prefix_account, new_dz_ip_net).is_ok() {
                                found = true;
                                #[cfg(test)]
                                msg!("Allocated new dz_ip {} in DzPrefixBlock", new_dz_ip_net);
                                break;
                            }
                        }
                        if !found {
                            return Err(DoubleZeroError::AllocationFailed.into());
                        }
                    }
                }
            }

            user.dz_ip = new_dz_ip;
        }
    } else {
        // Legacy path: no resource accounts, just overwrite fields
        if let Some(value) = value.dz_ip {
            user.dz_ip = value;
        }
        if let Some(value) = value.tunnel_id {
            user.tunnel_id = value;
        }
        if let Some(value) = value.tunnel_net {
            user.tunnel_net = value;
        }
    }

    if let Some(value) = value.user_type {
        user.user_type = value;
    }
    if let Some(value) = value.cyoa_type {
        user.cyoa_type = value;
    }
    if let Some(value) = value.validator_pubkey {
        user.validator_pubkey = value;
    }
    if let Some(new_tenant_pk) = value.tenant_pk {
        // If tenant accounts are provided, update reference counts
        if let (Some(old_tenant_acc), Some(new_tenant_acc)) =
            (old_tenant_account, new_tenant_account)
        {
            // Validate old tenant matches current user tenant
            assert_eq!(
                old_tenant_acc.key, &user.tenant_pk,
                "Old tenant account doesn't match current user tenant"
            );

            // Validate new tenant matches the requested tenant
            assert_eq!(
                new_tenant_acc.key, &new_tenant_pk,
                "New tenant account doesn't match requested tenant"
            );

            // Check account ownership
            assert_eq!(
                old_tenant_acc.owner, program_id,
                "Invalid Old Tenant Account Owner"
            );
            assert_eq!(
                new_tenant_acc.owner, program_id,
                "Invalid New Tenant Account Owner"
            );

            // Check writability
            assert!(
                old_tenant_acc.is_writable,
                "Old Tenant Account is not writable"
            );
            assert!(
                new_tenant_acc.is_writable,
                "New Tenant Account is not writable"
            );

            // Update reference counts
            let mut old_tenant = Tenant::try_from(old_tenant_acc)?;
            let mut new_tenant = Tenant::try_from(new_tenant_acc)?;

            // Decrement old tenant reference count
            old_tenant.reference_count = old_tenant.reference_count.saturating_sub(1);

            // Increment new tenant reference count
            new_tenant.reference_count = new_tenant
                .reference_count
                .checked_add(1)
                .ok_or(DoubleZeroError::InvalidIndex)?;

            // Write updated tenants
            try_acc_write(&old_tenant, old_tenant_acc, payer_account, accounts)?;
            try_acc_write(&new_tenant, new_tenant_acc, payer_account, accounts)?;
        }

        user.tenant_pk = new_tenant_pk;
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", user);

    Ok(())
}
