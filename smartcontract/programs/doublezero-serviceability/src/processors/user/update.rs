use crate::{
    authorize::{authorize, split_trailing_permission},
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
    state::{globalstate::GlobalState, permission::permission_flags, tenant::Tenant, user::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
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
    /// Number of DzPrefixBlock accounts passed for on-chain (de)allocation. Must be > 0:
    /// UpdateUser always reconciles the ResourceExtension bitmaps when changing
    /// dz_ip / tunnel_id / tunnel_net.
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
    /// Whether the MulticastPublisherBlock account is supplied (1 = yes, 0 = no).
    #[incremental(default = 0)]
    pub multicast_publisher_count: u8,
    pub tunnel_endpoint: Option<Ipv4Addr>,
}

impl fmt::Debug for UserUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, dz_ip: {}, tunnel_id: {}, tunnel_net: {}, validator_pubkey: {}, tenant_pk: {}, dz_prefix_count: {}, multicast_publisher_count: {}, tunnel_endpoint: {}",
            format_option!(self.user_type),
            format_option!(self.cyoa_type),
            format_option!(self.dz_ip),
            format_option!(self.tunnel_id),
            format_option!(self.tunnel_net),
            format_option!(self.validator_pubkey),
            format_option!(self.tenant_pk),
            self.dz_prefix_count,
            self.multicast_publisher_count,
            format_option!(self.tunnel_endpoint),
        )
    }
}

pub fn process_update_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserUpdateArgs,
) -> ProgramResult {
    if value.dz_prefix_count == 0 {
        #[cfg(test)]
        msg!("dz_prefix_count must be > 0; UpdateUser requires on-chain allocation");
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Account layout:
    //   [user, globalstate,
    //    user_tunnel_block, multicast_publisher_block?, device_tunnel_ids, dz_prefix_0..N,
    //    old_tenant?, new_tenant?, payer, system]
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

    // Remaining tail: [old_tenant?, new_tenant?, payer, system, permission?]. The
    // optional tenant pair is present when tenant_pk is being updated.
    // split_trailing_permission peels payer/system — and the optional payer
    // Permission PDA the SDK appends when it exists — off the tail by PDA match, so
    // the tenant accounts are detected unambiguously via what's left (`leading`).
    let remaining: Vec<&AccountInfo> = accounts_iter.collect();
    let (payer_account, _system_program, leading, permission_account) =
        split_trailing_permission(program_id, &remaining)?;
    // Exact-match the tenant slot: after peeling payer/system/permission and consuming
    // the fixed/variable prefix above, `leading` is either empty (no tenant change) or
    // exactly [old_tenant, new_tenant]. Reject any other count rather than silently
    // ignoring stray accounts.
    let (old_tenant_account, new_tenant_account) = match leading.len() {
        0 => (None, None),
        2 => (Some(leading[0]), Some(leading[1])),
        n => {
            msg!(
                "Unexpected account count: {} tenant accounts, expected 0 (no tenant change) \
                 or 2 (old_tenant/new_tenant)",
                n
            );
            return Err(DoubleZeroError::InvalidArgument.into());
        }
    };

    #[cfg(test)]
    msg!("process_update_user({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(user_account, program_id, writable = true, "User");
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        "GlobalState"
    );

    // Authorization: USER_ADMIN (Permission account) or foundation (legacy). This is
    // an administrative operation — the User owner does not update via this path.
    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        &mut permission_account.into_iter(),
        payer_account.key,
        &globalstate,
        permission_flags::USER_ADMIN,
    )?;

    let mut user: User = User::try_from(user_account)?;

    // Validate UserTunnelBlock PDA
    let (expected_user_tunnel_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
    validate_program_account!(
        user_tunnel_block_ext,
        program_id,
        writable = true,
        pda = &expected_user_tunnel_pda,
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
            pda = &expected_multicast_publisher_pda,
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
        pda = &expected_tunnel_ids_pda,
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
            pda = &expected_dz_prefix_pda,
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

    if let Some(value) = value.user_type {
        user.user_type = value;
    }
    if let Some(value) = value.cyoa_type {
        user.cyoa_type = value;
    }
    if let Some(value) = value.validator_pubkey {
        user.validator_pubkey = value;
    }
    if let Some(value) = value.tunnel_endpoint {
        user.tunnel_endpoint = value;
    }
    if let Some(new_tenant_pk) = value.tenant_pk {
        // If tenant accounts are provided, update reference counts
        if let (Some(old_tenant_acc), Some(new_tenant_acc)) =
            (old_tenant_account, new_tenant_account)
        {
            // Validate new tenant matches the requested tenant
            assert_eq!(
                new_tenant_acc.key, &new_tenant_pk,
                "New tenant account doesn't match requested tenant"
            );
            assert_eq!(
                new_tenant_acc.owner, program_id,
                "Invalid New Tenant Account Owner"
            );
            assert!(
                new_tenant_acc.is_writable,
                "New Tenant Account is not writable"
            );

            // Increment new tenant reference count
            let mut new_tenant = Tenant::try_from(new_tenant_acc)?;
            new_tenant.reference_count = new_tenant
                .reference_count
                .checked_add(1)
                .ok_or(DoubleZeroError::InvalidIndex)?;
            try_acc_write(&new_tenant, new_tenant_acc, payer_account, accounts)?;

            // Skip old tenant when its key is Pubkey::default() — placeholder used
            // for initial tenant assignment, when the user has no prior tenant.
            if old_tenant_acc.key != &Pubkey::default() {
                // Validate old tenant matches current user tenant
                assert_eq!(
                    old_tenant_acc.key, &user.tenant_pk,
                    "Old tenant account doesn't match current user tenant"
                );
                assert_eq!(
                    old_tenant_acc.owner, program_id,
                    "Invalid Old Tenant Account Owner"
                );
                assert!(
                    old_tenant_acc.is_writable,
                    "Old Tenant Account is not writable"
                );

                // Decrement old tenant reference count
                let mut old_tenant = Tenant::try_from(old_tenant_acc)?;
                old_tenant.reference_count = old_tenant.reference_count.saturating_sub(1);
                try_acc_write(&old_tenant, old_tenant_acc, payer_account, accounts)?;
            }
        }

        user.tenant_pk = new_tenant_pk;
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", user);

    Ok(())
}
