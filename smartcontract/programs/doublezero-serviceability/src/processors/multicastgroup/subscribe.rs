use crate::{
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
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
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
pub struct MulticastGroupSubscribeArgs {
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr,
    pub publisher: bool,
    pub subscriber: bool,
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
}

impl fmt::Debug for MulticastGroupSubscribeArgs {
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
    /// trigger activator reprocessing should check this flag.
    pub publisher_list_transitioned: bool,
}

/// Toggle a user's multicast group subscription.
///
/// Handles both create-time subscription (user lists start empty, only adds)
/// and post-activation subscription changes (add/remove toggle). The caller is
/// responsible for setting `user.status = Updating` when
/// `publisher_list_transitioned` is true and the user is already activated.
pub fn update_user_multicastgroup_subscription(
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

    // Check allowlists for additions
    if publisher && !accesspass.mgroup_pub_allowlist.contains(mgroup_account.key) {
        msg!("{:?}", accesspass);
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if subscriber && !accesspass.mgroup_sub_allowlist.contains(mgroup_account.key) {
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

pub fn process_update_multicastgroup_subscription(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupSubscribeArgs,
) -> ProgramResult {
    let num_accounts = accounts.len();
    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let user_account = next_account_info(accounts_iter)?;

    // Account layout WITH onchain allocation (use_onchain_allocation=true):
    //   [mgroup, accesspass, user, globalstate, multicast_publisher_block, payer, system]
    // Account layout WITHOUT onchain allocation, WITH globalstate (num_accounts >= 6):
    //   [mgroup, accesspass, user, globalstate, payer, system]
    // Account layout WITHOUT onchain allocation, WITHOUT globalstate (num_accounts == 5, backward compat):
    //   [mgroup, accesspass, user, payer, system]
    let has_globalstate = value.use_onchain_allocation || num_accounts >= 6;
    let globalstate_opt = if has_globalstate {
        let gs_account = next_account_info(accounts_iter)?;
        let (expected_globalstate_pda, _) = get_globalstate_pda(program_id);
        assert_eq!(
            gs_account.key, &expected_globalstate_pda,
            "Invalid GlobalState PDA"
        );
        Some(GlobalState::try_from(gs_account)?)
    } else {
        None
    };
    let onchain_accounts = if value.use_onchain_allocation {
        let multicast_publisher_block_ext = next_account_info(accounts_iter)?;
        Some(multicast_publisher_block_ext)
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_multicastgroup_subscription({:?})", value);

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
    // Unsubscribe is allowed for any status so that users
    // created via CreateSubscribeUser can be cleaned up before activation.
    let is_subscribe = value.publisher || value.subscriber;
    if is_subscribe && user.status != UserStatus::Activated && user.status != UserStatus::Updating {
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

    if !accesspass.allow_multiple_ip() && accesspass.client_ip != user.client_ip {
        msg!(
            "AccessPass client_ip does not match. accesspass.client_ip: {} user.client_ip: {}",
            accesspass.client_ip,
            user.client_ip
        );
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // The access pass must belong to the payer. If the payer differs, the payer
    // must be in the foundation allowlist — which requires globalstate to be provided.
    // Callers without globalstate (num_accounts < 6) are only permitted when
    // payer == accesspass.user_payer (backward compatible with old clients).
    if accesspass.user_payer != *payer_account.key {
        let in_foundation = globalstate_opt
            .as_ref()
            .map(|gs| gs.foundation_allowlist.contains(payer_account.key))
            .unwrap_or(false);
        if !in_foundation {
            msg!(
                "AccessPass user_payer {:?} does not match payer {:?}",
                accesspass.user_payer,
                payer_account.key
            );
            return Err(DoubleZeroError::Unauthorized.into());
        }
    }

    let result = update_user_multicastgroup_subscription(
        mgroup_account,
        &accesspass,
        &mut user,
        value.publisher,
        value.subscriber,
    )?;

    if let Some(multicast_publisher_block_ext) = onchain_accounts {
        // Onchain allocation path: allocate dz_ip directly, skip Updating status
        let globalstate = globalstate_opt
            .as_ref()
            .expect("globalstate required for onchain allocation");
        if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
            return Err(DoubleZeroError::FeatureNotEnabled.into());
        }

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
    } else {
        // Legacy path: trigger activator reprocessing when publisher list transitions
        // (gaining first publisher requires dz_ip allocation, losing last means it's no longer needed).
        // Skip for Pending users — they haven't been activated yet so there is
        // no dz_ip to (de)allocate and the Updating status would fail validation.
        if result.publisher_list_transitioned && user.status != UserStatus::Pending {
            user.status = UserStatus::Updating;
        }
    }

    try_acc_write(&result.mgroup, mgroup_account, payer_account, accounts)?;
    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
