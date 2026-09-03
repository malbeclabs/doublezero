use crate::{
    authorize::{authorize, split_trailing_permission},
    error::DoubleZeroError,
    pda::{get_accesspass_pda, get_globalstate_pda, get_resource_extension_pda},
    processors::{
        resource::{allocate_ip, deallocate_ip},
        validation::validate_program_account,
    },
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        accesspass::AccessPass,
        globalstate::GlobalState,
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
        permission::permission_flags,
        user::{User, UserStatus},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct UpdateMulticastGroupRolesArgs {
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr,
    pub publisher: bool,
    pub subscriber: bool,
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
    /// Number of additional writable MulticastGroup accounts following the five fixed
    /// accounts. The role change is applied to the primary group plus every extra
    /// group atomically. Old encodings without this byte decode as 0 (single-group
    /// behavior).
    #[incremental(default = 0)]
    pub extra_group_count: u8,
}

impl fmt::Debug for UpdateMulticastGroupRolesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "client_ip: {}, publisher: {:?}, subscriber: {:?}, use_onchain_allocation: {:?}, extra_group_count: {}",
            self.client_ip, self.publisher, self.subscriber, self.use_onchain_allocation, self.extra_group_count
        )
    }
}

pub struct SubscribeUserResult {
    pub mgroup: MulticastGroup,
    /// True if the publisher list transitioned between empty and non-empty
    /// (gained first publisher or lost last publisher).
    pub publisher_list_transitioned: bool,
}

/// Authorize a role grant against the access pass's multicast-group allowlists.
///
/// This is the authorization used by every access-pass type that grants groups individually. The
/// only alternative is the EdgeSeat feed metro gate, which derives joinable groups from the feeds
/// provisioned on the pass; a caller runs one or the other, never neither. Removals are always
/// allowed, so a user can be cleaned up after a group leaves an allowlist.
pub fn check_mgroup_allowlists(
    accesspass: &AccessPass,
    mgroup_key: &Pubkey,
    publisher: bool,
    subscriber: bool,
) -> ProgramResult {
    if publisher && !accesspass.mgroup_pub_allowlist.contains(mgroup_key) {
        msg!("{:?}", accesspass);
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if subscriber && !accesspass.mgroup_sub_allowlist.contains(mgroup_key) {
        msg!("{:?}", accesspass);
        return Err(DoubleZeroError::NotAllowed.into());
    }
    Ok(())
}

/// Set a user's multicast group roles to the requested state.
///
/// `publisher` and `subscriber` are desired states, not toggle signals: `true` adds the role when the
/// user does not hold it, `false` removes it when they do, and either is a no-op otherwise.
///
/// Mechanics only: this does NOT authorize the change. Callers must first run either
/// [`check_mgroup_allowlists`] or the EdgeSeat feed metro gate, so that the authorization a
/// processor performed is visible in that processor rather than claimed through an argument here.
///
/// Handles both create-time subscription (user lists start empty, only adds)
/// and post-activation subscription changes. The caller is
/// responsible for setting `user.status = Updating` when
/// `publisher_list_transitioned` is true and the user is already activated.
pub fn update_user_multicastgroup_roles(
    mgroup_account: &AccountInfo,
    user: &mut User,
    publisher: bool,
    subscriber: bool,
) -> Result<SubscribeUserResult, ProgramError> {
    let mut mgroup = MulticastGroup::try_from(mgroup_account)?;
    if mgroup.status != MulticastGroupStatus::Activated {
        msg!("MulticastGroupStatus: {:?}", mgroup.status);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let mut publisher_list_transitioned = false;

    // Manage the publisher list
    match publisher {
        true => {
            if !user.publishers.contains(mgroup_account.key) {
                let was_empty = user.publishers.is_empty();
                mgroup.publisher_count = mgroup.publisher_count.saturating_add(1);
                user.publishers.push(*mgroup_account.key);
                if was_empty {
                    publisher_list_transitioned = true;
                }
            }
        }
        false => {
            if user.publishers.contains(mgroup_account.key) {
                mgroup.publisher_count = mgroup.publisher_count.saturating_sub(1);
                user.publishers.retain(|&x| x != *mgroup_account.key);
                if user.publishers.is_empty() {
                    publisher_list_transitioned = true;
                }
            }
        }
    }

    // Manage the subscriber list
    match subscriber {
        true => {
            if !user.subscribers.contains(mgroup_account.key) {
                mgroup.subscriber_count = mgroup.subscriber_count.saturating_add(1);
                user.subscribers.push(*mgroup_account.key);
            }
        }
        false => {
            if user.subscribers.contains(mgroup_account.key) {
                mgroup.subscriber_count = mgroup.subscriber_count.saturating_sub(1);
                user.subscribers.retain(|&x| x != *mgroup_account.key);
            }
        }
    }

    Ok(SubscribeUserResult {
        mgroup,
        publisher_list_transitioned,
    })
}

pub fn process_update_multicastgroup_roles(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UpdateMulticastGroupRolesArgs,
) -> ProgramResult {
    if !value.use_onchain_allocation {
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let user_account = next_account_info(accounts_iter)?;

    // Account layout: [mgroup, accesspass, user, globalstate, multicast_publisher_block, payer, system]
    let gs_account = next_account_info(accounts_iter)?;
    let (expected_globalstate_pda, _) = get_globalstate_pda(program_id);
    assert_eq!(
        gs_account.key, &expected_globalstate_pda,
        "Invalid GlobalState PDA"
    );
    let globalstate = GlobalState::try_from(gs_account)?;
    let multicast_publisher_block_ext = next_account_info(accounts_iter)?;

    // Trailing layout: [mgroup₁..mgroupₙ, payer, system, permission?]. The extra
    // multicast groups (batch role change, counted by args.extra_group_count) come
    // first; the SDK appends the payer's Permission PDA last (via
    // execute_authorized_transaction), and split_trailing_permission identifies it by
    // PDA match rather than by position, so the variable-length extras never confuse it.
    let remaining: Vec<&AccountInfo> = accounts_iter.collect();
    let (payer_account, system_program, leading, permission_account) =
        split_trailing_permission(program_id, &remaining)?;
    let extra_group_count = value.extra_group_count as usize;
    if extra_group_count > leading.len() {
        msg!(
            "extra_group_count {} exceeds {} supplied accounts",
            extra_group_count,
            leading.len()
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    let (extra_group_accounts, _rest) = leading.split_at(extra_group_count);

    #[cfg(test)]
    msg!("process_update_multicastgroup_roles({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(
        mgroup_account,
        program_id,
        writable = true,
        "MulticastGroup"
    );
    for extra_group_account in extra_group_accounts {
        validate_program_account!(
            *extra_group_account,
            program_id,
            writable = true,
            "MulticastGroup"
        );
    }
    // Reject duplicate group accounts. A duplicate aliases the same account data
    // twice in the batch loop, making the final counter state depend on write
    // ordering — an explicit error is a clearer contract than an accidental no-op.
    for (i, group_key) in std::iter::once(mgroup_account.key)
        .chain(extra_group_accounts.iter().map(|a| a.key))
        .enumerate()
    {
        if extra_group_accounts[i..].iter().any(|a| a.key == group_key) {
            msg!("duplicate multicast group {} in batch", group_key);
            return Err(DoubleZeroError::InvalidArgument.into());
        }
    }
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    validate_program_account!(
        accesspass_account,
        program_id,
        writable = false,
        "AccessPass"
    );
    validate_program_account!(user_account, program_id, writable = true, "User");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    // Parse and validate user
    let mut user: User = User::try_from(user_account)?;
    // Removing all roles is allowed for any status so that users
    // created via CreateSubscribeUser can be cleaned up before activation.
    let has_role = value.publisher || value.subscriber;
    if has_role && user.status != UserStatus::Activated {
        msg!("UserStatus: {:?}", user.status);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let accesspass = AccessPass::try_from(accesspass_account)?;

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &user.client_ip, &user.owner);
    let (accesspass_dynamic_pda, _) =
        get_accesspass_pda(program_id, &Ipv4Addr::UNSPECIFIED, &user.owner);
    assert!(
        accesspass_account.key == &accesspass_pda
            || accesspass_account.key == &accesspass_dynamic_pda,
        "Invalid AccessPass PDA",
    );

    // The access pass must belong to the payer. If the payer differs, the payer
    // must be a foundation member, or — for removal-only cleanup (no roles being
    // granted) — hold USER_ADMIN. The USER_ADMIN path lets an operator strip a
    // user's multicast roles as a prerequisite to deleting/request-banning that
    // user.
    if accesspass.user_payer != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        // A caller who is neither the pass's user_payer nor a foundation member may still act on
        // another owner's pass with the right permission, and the two operations require different
        // grants:
        //   - Removal-only cleanup (stripping roles as a prerequisite to delete/request-ban) is a
        //     USER_ADMIN operation, as the Delete<Kind>User instructions / RequestBanUserCommand
        //     authorize the final instruction with the same flag.
        //   - Granting roles (subscribe/publish) on behalf of another owner manages the pass's
        //     entitlements, so it is an ACCESS_PASS_ADMIN operation. This is the path the oracle
        //     uses to subscribe validator-owned users (accesspass.user_payer = validator) once it
        //     drops out of foundation and operates on its Permission account.
        // The oracle holds both flags. authorize() reads the optional trailing Permission account
        // the SDK appends and also honors the corresponding legacy authorities.
        let removal_only = !value.publisher && !value.subscriber;
        let required_flag = if removal_only {
            permission_flags::USER_ADMIN
        } else {
            permission_flags::ACCESS_PASS_ADMIN
        };
        let authorized = authorize(
            program_id,
            &mut permission_account.into_iter(),
            payer_account.key,
            &globalstate,
            required_flag,
        )
        .is_ok();
        if !authorized {
            if !removal_only {
                msg!(
                    "AccessPass user_payer {:?} does not match payer {:?}",
                    accesspass.user_payer,
                    payer_account.key
                );
            }
            // Preserve the historical error variants: a removal-only cleanup that fails
            // authorization returns NotAllowed (as the prior `authorize()?` did), while an
            // attempt to add roles without authority returns Unauthorized.
            return Err(if removal_only {
                DoubleZeroError::NotAllowed.into()
            } else {
                DoubleZeroError::Unauthorized.into()
            });
        }
    }

    // Apply the role change to every group in the batch. Every pass type is
    // authorized the same way here: each group must be on the pass's allowlist. Any
    // per-group failure aborts the whole instruction, so the batch is atomic.
    // Aggregating the transition flag with `|=` is correct for the dz_ip logic
    // below: on batch adds only the first add sees empty→non-empty; on batch
    // removes only the last removal sees non-empty→empty.
    let mut publisher_list_transitioned = false;
    for group_account in std::iter::once(mgroup_account).chain(extra_group_accounts.iter().copied())
    {
        check_mgroup_allowlists(
            &accesspass,
            group_account.key,
            value.publisher,
            value.subscriber,
        )?;
        let result = update_user_multicastgroup_roles(
            group_account,
            &mut user,
            value.publisher,
            value.subscriber,
        )?;
        publisher_list_transitioned |= result.publisher_list_transitioned;
        try_acc_write(&result.mgroup, group_account, payer_account, accounts)?;
    }

    // Allocate dz_ip when gaining first publisher
    if publisher_list_transitioned
        && value.publisher
        && (user.dz_ip == Ipv4Addr::UNSPECIFIED || user.dz_ip == user.client_ip)
    {
        let (expected_multicast_publisher_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
        validate_program_account!(
            multicast_publisher_block_ext,
            program_id,
            writable = true,
            pda = &expected_multicast_publisher_pda,
            "MulticastPublisherBlock"
        );

        user.dz_ip = allocate_ip(multicast_publisher_block_ext, 1)?.ip();
    } else if publisher_list_transitioned
        && !value.publisher
        && user.dz_ip != Ipv4Addr::UNSPECIFIED
        && user.dz_ip != user.client_ip
    {
        // Deallocate dz_ip back to MulticastPublisherBlock
        let (expected_multicast_publisher_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
        validate_program_account!(
            multicast_publisher_block_ext,
            program_id,
            writable = true,
            pda = &expected_multicast_publisher_pda,
            "MulticastPublisherBlock"
        );

        if let Ok(dz_ip_net) = NetworkV4::new(user.dz_ip, 32) {
            deallocate_ip(multicast_publisher_block_ext, dz_ip_net);
        }
        user.dz_ip = user.client_ip;
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
