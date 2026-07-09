use crate::{
    authorize::{authorize, split_trailing_permission},
    error::DoubleZeroError,
    pda::{get_accesspass_pda, get_globalstate_pda, get_resource_extension_pda},
    processors::{
        feed::check_feed_metro_coverage,
        resource::{allocate_ip, deallocate_ip},
        validation::validate_program_account,
    },
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        accesspass::{AccessPass, AccessPassType},
        device::Device,
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
}

impl fmt::Debug for UpdateMulticastGroupRolesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "client_ip: {}, publisher: {:?}, subscriber: {:?}, use_onchain_allocation: {:?}",
            self.client_ip, self.publisher, self.subscriber, self.use_onchain_allocation
        )
    }
}

pub struct SubscribeUserResult {
    pub mgroup: MulticastGroup,
    /// True if the publisher list transitioned between empty and non-empty
    /// (gained first publisher or lost last publisher). Callers that need to
    /// trigger downstream reprocessing should check this flag.
    pub publisher_list_transitioned: bool,
}

/// Toggle a user's multicast group roles.
///
/// Handles both create-time subscription (user lists start empty, only adds)
/// and post-activation subscription changes (add/remove toggle). The caller is
/// responsible for setting `user.status = Updating` when
/// `publisher_list_transitioned` is true and the user is already activated.
pub fn update_user_multicastgroup_roles(
    mgroup_account: &AccountInfo,
    accesspass: &AccessPass,
    user: &mut User,
    publisher: bool,
    subscriber: bool,
) -> Result<SubscribeUserResult, ProgramError> {
    let mut mgroup = MulticastGroup::try_from(mgroup_account)?;
    if mgroup.status != MulticastGroupStatus::Activated {
        msg!("MulticastGroupStatus: {:?}", mgroup.status);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // Check allowlists for additions. EdgeSeat passes derive joinable groups from their feeds'
    // metro→group map (the feed metro gate), which supersedes the mgroup allowlist; the caller is
    // responsible for running enforce_feed_metro_gate for EdgeSeat connects.
    let is_edge_seat = matches!(accesspass.accesspass_type, AccessPassType::EdgeSeat(_));
    if publisher && !is_edge_seat && !accesspass.mgroup_pub_allowlist.contains(mgroup_account.key) {
        msg!("{:?}", accesspass);
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if subscriber && !is_edge_seat && !accesspass.mgroup_sub_allowlist.contains(mgroup_account.key)
    {
        msg!("{:?}", accesspass);
        return Err(DoubleZeroError::NotAllowed.into());
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

    // Trailing layout: [device?, feed?, payer, system, permission?]. The SDK appends the payer's
    // Permission PDA last (via execute_authorized_transaction); the optional EdgeSeat device/feed
    // accounts for post-activation metro re-gating precede payer/system, because the client pushes
    // them into the instruction's account list ahead of the [payer, system, permission] trailer
    // that assemble_instructions always appends. split_trailing_permission identifies the
    // Permission by PDA match rather than by position, so it never mistakes device/feed for the
    // Permission account regardless of which optional accounts are present.
    let remaining: Vec<&AccountInfo> = accounts_iter.collect();
    let (payer_account, system_program, leading, permission_account) =
        split_trailing_permission(program_id, &remaining)?;
    let device_account = leading.first().copied();
    let feed_account = leading.get(1).copied();

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
    // user (see DeleteUserCommand / RequestBanUserCommand, which authorize the
    // final instruction with the same USER_ADMIN flag).
    if accesspass.user_payer != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        // A caller who is neither the pass's user_payer nor a foundation member may still act on
        // another owner's pass with the right permission, and the two operations require different
        // grants:
        //   - Removal-only cleanup (stripping roles as a prerequisite to delete/request-ban) is a
        //     USER_ADMIN operation, as DeleteUserCommand / RequestBanUserCommand authorize the
        //     final instruction with the same flag.
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

    // EdgeSeat passes derive joinable groups from their feeds' metro→group map. The seat was
    // ticked at connect (CreateSubscribeUser), so post-activation role adds only re-validate
    // coverage; they do not re-tick.
    if matches!(accesspass.accesspass_type, AccessPassType::EdgeSeat(_))
        && (value.publisher || value.subscriber)
    {
        let device_account = device_account.ok_or(DoubleZeroError::MetroMismatch)?;
        validate_program_account!(device_account, program_id, writable = false, "Device");
        if user.device_pk != *device_account.key {
            return Err(ProgramError::InvalidAccountData);
        }
        let device = Device::try_from(device_account)?;
        check_feed_metro_coverage(
            program_id,
            &accesspass,
            &device.exchange_pk,
            Some(mgroup_account.key),
            feed_account,
        )?;
    }

    let result = update_user_multicastgroup_roles(
        mgroup_account,
        &accesspass,
        &mut user,
        value.publisher,
        value.subscriber,
    )?;

    // Allocate dz_ip when gaining first publisher
    if result.publisher_list_transitioned
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
    } else if result.publisher_list_transitioned
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

    try_acc_write(&result.mgroup, mgroup_account, payer_account, accounts)?;
    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
