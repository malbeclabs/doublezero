use crate::{
    error::DoubleZeroError,
    pda::{get_accesspass_pda, get_mgroup_allowlist_entry_pda},
    seeds::{SEED_MGROUP_ALLOWLIST, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accesspass::AccessPass,
        accounttype::AccountType,
        mgroup_allowlist_entry::{
            is_valid_mgroup_allowlist_entry, MGroupAllowlistEntry, MGroupAllowlistType,
        },
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
        user::{User, UserStatus},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};

/// Check if an access pass is allowed for a multicast group (PDA-first with Vec fallback + self-migration).
/// Returns true if the Vec was modified and the access pass needs to be written back.
#[allow(clippy::too_many_arguments)]
pub(crate) fn check_and_migrate_allowlist<'a>(
    program_id: &Pubkey,
    accesspass_account: &AccountInfo<'a>,
    mgroup_account: &AccountInfo<'a>,
    al_entry_account: &AccountInfo<'a>,
    payer_account: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    allowlist_type: MGroupAllowlistType,
    allowlist_vec: &mut Vec<Pubkey>,
) -> Result<bool, solana_program::program_error::ProgramError> {
    let (expected_pda, bump) = get_mgroup_allowlist_entry_pda(
        program_id,
        accesspass_account.key,
        mgroup_account.key,
        allowlist_type as u8,
    );
    if is_valid_mgroup_allowlist_entry(al_entry_account, &expected_pda, program_id) {
        // PDA exists -> allowed (fast path)
        Ok(false)
    } else if allowlist_vec.contains(mgroup_account.key) {
        // Found in Vec -> self-migrate: create PDA + swap_remove from Vec
        assert_eq!(
            al_entry_account.key, &expected_pda,
            "Invalid MGroupAllowlistEntry PDA for {:?}",
            allowlist_type
        );
        let al_entry = MGroupAllowlistEntry {
            account_type: AccountType::MGroupAllowlistEntry,
            bump_seed: bump,
            allowlist_type,
        };
        try_acc_create(
            &al_entry,
            al_entry_account,
            payer_account,
            system_program,
            program_id,
            &[
                SEED_PREFIX,
                SEED_MGROUP_ALLOWLIST,
                &accesspass_account.key.to_bytes(),
                &mgroup_account.key.to_bytes(),
                &[allowlist_type as u8],
                &[bump],
            ],
        )?;
        if let Some(pos) = allowlist_vec.iter().position(|k| k == mgroup_account.key) {
            allowlist_vec.swap_remove(pos);
        }
        Ok(true)
    } else {
        Err(DoubleZeroError::NotAllowed.into())
    }
}

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct MulticastGroupSubscribeArgs {
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr,
    pub publisher: bool,
    pub subscriber: bool,
}

impl fmt::Debug for MulticastGroupSubscribeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "client_ip: {}, publisher: {:?}, subscriber: {:?}",
            self.client_ip, self.publisher, self.subscriber
        )
    }
}

pub fn process_subscribe_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupSubscribeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let user_account = next_account_info(accounts_iter)?;
    let mgroup_pub_al_entry = next_account_info(accounts_iter)?;
    let mgroup_sub_al_entry = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_subscribe_multicastgroup({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        mgroup_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid Accesspass Account Owner"
    );
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    assert!(
        mgroup_account.is_writable,
        "multicastgroup account is not writable"
    );
    assert!(user_account.is_writable, "user account is not writable");

    // Parse accounts
    let mut mgroup: MulticastGroup = MulticastGroup::try_from(mgroup_account)?;
    if mgroup.status != MulticastGroupStatus::Activated {
        #[cfg(test)]
        msg!("MulticastGroupStatus: {:?}", mgroup.status);

        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let mut user: User = User::try_from(user_account)?;
    if user.status != UserStatus::Activated && user.status != UserStatus::Updating {
        msg!("UserStatus: {:?}", user.status);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let mut accesspass = AccessPass::try_from(accesspass_account)?;

    let ip_seed = if accesspass.allow_multiple_ip() {
        Ipv4Addr::UNSPECIFIED
    } else {
        user.client_ip
    };

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &ip_seed, payer_account.key);
    assert_eq!(
        accesspass_account.key, &accesspass_pda,
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

    // Check if the user is in the allowlist (PDA-first with Vec fallback + self-migration)
    let mut accesspass_modified = false;
    if value.publisher {
        accesspass_modified |= check_and_migrate_allowlist(
            program_id,
            accesspass_account,
            mgroup_account,
            mgroup_pub_al_entry,
            payer_account,
            system_program,
            MGroupAllowlistType::Publisher,
            &mut accesspass.mgroup_pub_allowlist,
        )
        .inspect_err(|_| {
            msg!("{:?}", accesspass);
        })?;
    }
    if value.subscriber {
        accesspass_modified |= check_and_migrate_allowlist(
            program_id,
            accesspass_account,
            mgroup_account,
            mgroup_sub_al_entry,
            payer_account,
            system_program,
            MGroupAllowlistType::Subscriber,
            &mut accesspass.mgroup_sub_allowlist,
        )
        .inspect_err(|_| {
            msg!("{:?}", accesspass);
        })?;
    }

    // Write back the accesspass if we migrated entries from Vec to PDA
    if accesspass_modified {
        try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;
    }

    // Manage the publisher lists
    match value.publisher {
        true => {
            if !user.publishers.contains(mgroup_account.key) {
                let was_empty = user.publishers.is_empty();
                // Increment publisher count
                mgroup.publisher_count = mgroup.publisher_count.saturating_add(1);
                // Add multicast group to user's publisher list
                user.publishers.push(*mgroup_account.key);
                // Only trigger activator reprocessing when gaining first publisher
                // (activator needs to allocate dz_ip)
                if was_empty {
                    user.status = UserStatus::Updating;
                }
            }
        }
        false => {
            if user.publishers.contains(mgroup_account.key) {
                // Decrement publisher count
                mgroup.publisher_count = mgroup.publisher_count.saturating_sub(1);
                // Remove multicast group from user's publisher list
                user.publishers.retain(|&x| x != *mgroup_account.key);
                // Trigger activator reprocessing when losing last publisher
                // (dz_ip no longer needed)
                if user.publishers.is_empty() {
                    user.status = UserStatus::Updating;
                }
            }
        }
    }

    // Manage the subscriber lists
    match value.subscriber {
        true => {
            if !user.subscribers.contains(mgroup_account.key) {
                // Increment subscriber count
                mgroup.subscriber_count = mgroup.subscriber_count.saturating_add(1);
                // Add multicast group to user's subscriber list
                user.subscribers.push(*mgroup_account.key);
                // No activator reprocessing needed for subscriber changes
                // (subscriber groups don't affect tunnel or dz_ip config)
            }
        }
        false => {
            if user.subscribers.contains(mgroup_account.key) {
                // Decrement subscriber count
                mgroup.subscriber_count = mgroup.subscriber_count.saturating_sub(1);
                // Remove multicast group from user's subscriber list
                user.subscribers.retain(|&x| x != *mgroup_account.key);
            }
        }
    }

    try_acc_write(&mgroup, mgroup_account, payer_account, accounts)?;
    try_acc_write(&user, user_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", mgroup);
    #[cfg(test)]
    msg!("Updated: {:?}", user_account);

    Ok(())
}
