use crate::{
    error::DoubleZeroError,
    pda::*,
    seeds::{SEED_ACCESS_PASS, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType, ALLOW_MULTIPLE_IP, IS_DYNAMIC},
        accounttype::AccountType,
        globalstate::GlobalState,
        tenant::Tenant,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    clock::Clock,
    entrypoint::ProgramResult,
    msg,
    program::invoke_signed_unchecked,
    pubkey::Pubkey,
    rent::Rent,
    sysvar::Sysvar,
};

use std::net::Ipv4Addr;

// Value to rent exempt two `User` accounts + configurable amount for connect/disconnect txns
// `User` account size assumes a single publisher and subscriber pubkey registered
const AIRDROP_USER_RENT_LAMPORTS_BYTES: usize = 240 * 3; // 240 bytes per User account x 3 accounts = 720 bytes

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct SetAccessPassArgs {
    pub accesspass_type: AccessPassType, // 1 or 33
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr, // 4
    pub last_access_epoch: u64,          // 8
    pub allow_multiple_ip: bool,         // 1
}

impl fmt::Debug for SetAccessPassArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "accesspass_type: {}, ip: {}, last_access_epoch: {}, allow_multiple_ip: {}",
            self.accesspass_type, self.client_ip, self.last_access_epoch, self.allow_multiple_ip,
        )
    }
}

pub fn process_set_access_pass(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetAccessPassArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let user_payer = next_account_info(accounts_iter)?;

    // Optional tenant accounts for reference counting (backwards compatible)
    let (tenant_remove_account, tenant_add_account) = if accounts.len() >= 7 {
        let remove = next_account_info(accounts_iter)?;
        let add = next_account_info(accounts_iter)?;
        (Some(remove), Some(add))
    } else {
        (None, None)
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_accesspass({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        *globalstate_account.owner,
        program_id.clone(),
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(
        accesspass_account.is_writable,
        "PDA Account is not writable"
    );

    let (expected_pda_account, bump_seed) =
        get_accesspass_pda(program_id, &value.client_ip, user_payer.key);
    assert_eq!(
        accesspass_account.key, &expected_pda_account,
        "Invalid AccessPass PubKey"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if globalstate.sentinel_authority_pk != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        msg!(
            "sentinel_authority_pk: {} payer: {} foundation_allowlist: {:?}",
            globalstate.sentinel_authority_pk,
            payer_account.key,
            globalstate.foundation_allowlist
        );
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let AccessPassType::SolanaValidator(node_id) = value.accesspass_type {
        if node_id == Pubkey::default() {
            msg!("Solana validator access pass type requires a validator pubkey");
            return Err(DoubleZeroError::InvalidSolanaPubkey.into());
        }
    }

    let clock = Clock::get()?;
    let current_epoch = clock.epoch;

    if value.last_access_epoch > 0 && value.last_access_epoch < current_epoch {
        return Err(DoubleZeroError::InvalidLastAccessEpoch.into());
    }

    // Flags
    let mut flags = 0;
    if value.client_ip == Ipv4Addr::UNSPECIFIED {
        flags |= IS_DYNAMIC;
    }
    if value.allow_multiple_ip {
        flags |= ALLOW_MULTIPLE_IP;
    }

    // If account does not exist, create it
    if *accesspass_account.owner == solana_system_interface::program::ID {
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed,
            accesspass_type: value.accesspass_type.clone(),
            client_ip: value.client_ip,
            user_payer: *user_payer.key,
            last_access_epoch: value.last_access_epoch,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: *payer_account.key,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: if let Some(acc) = tenant_add_account {
                vec![*acc.key]
            } else {
                vec![]
            },
            flags,
        };

        try_acc_create(
            &accesspass,
            accesspass_account,
            payer_account,
            system_program,
            program_id,
            &[
                SEED_PREFIX,
                SEED_ACCESS_PASS,
                &value.client_ip.octets(),
                &user_payer.key.to_bytes(),
                &[bump_seed],
            ],
        )?;

        #[cfg(test)]
        msg!("Created: {:?}", accesspass);
    } else {
        // Read or create Access Pass
        // Old bug where close accounts were not fully zeroed out instead of being closed
        let mut accesspass = if !accesspass_account.data_is_empty() {
            assert_eq!(
                accesspass_account.owner, program_id,
                "Invalid PDA Account Owner"
            );

            AccessPass::try_from(accesspass_account)?
        } else {
            AccessPass {
                account_type: AccountType::AccessPass,
                bump_seed,
                accesspass_type: value.accesspass_type.clone(),
                client_ip: value.client_ip,
                flags,
                user_payer: *user_payer.key,
                last_access_epoch: value.last_access_epoch,
                connection_count: 0,
                status: AccessPassStatus::Requested,
                owner: *payer_account.key,
                mgroup_pub_allowlist: vec![],
                mgroup_sub_allowlist: vec![],
                tenant_allowlist: vec![],
            }
        };

        // Update fields
        accesspass.accesspass_type = value.accesspass_type.clone();
        accesspass.last_access_epoch = value.last_access_epoch;
        accesspass.flags = flags;

        if let Some(tenant_remove) = tenant_remove_account {
            accesspass
                .tenant_allowlist
                .retain(|&x| x != *tenant_remove.key);
        }
        if let Some(tenant_add) = tenant_add_account {
            if tenant_add.key != &Pubkey::default()
                && !accesspass.tenant_allowlist.contains(tenant_add.key)
            {
                accesspass.tenant_allowlist.push(*tenant_add.key);
            }
        }

        // Write back updated Access Pass
        try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

        #[cfg(test)]
        msg!("Updated: {:?}", accesspass);
    }

    // Manage tenant reference counting for added/removed tenants (if provided)
    if let Some(tenant_remove_acc) = tenant_remove_account {
        if tenant_remove_acc.key != &Pubkey::default() {
            // Validate removed tenant account
            assert_eq!(
                *tenant_remove_acc.owner, *program_id,
                "Invalid Tenant Remove Account Owner"
            );
            assert!(
                tenant_remove_acc.is_writable,
                "Tenant Remove Account is not writable"
            );
            let mut tenant_remove = Tenant::try_from(tenant_remove_acc)?;
            tenant_remove.reference_count = tenant_remove
                .reference_count
                .checked_sub(1)
                .ok_or(DoubleZeroError::InvalidIndex)?;
            try_acc_write(&tenant_remove, tenant_remove_acc, payer_account, accounts)?;
        }
    }

    if let Some(tenant_add_acc) = tenant_add_account {
        if tenant_add_acc.key != &Pubkey::default() {
            // Validate added tenant account
            assert_eq!(
                *tenant_add_acc.owner, *program_id,
                "Invalid Tenant Add Account Owner"
            );
            assert!(
                tenant_add_acc.is_writable,
                "Tenant Add Account is not writable"
            );
            let mut tenant_add = Tenant::try_from(tenant_add_acc)?;

            // Validate payer is administrator of the tenant
            if !tenant_add.administrators.contains(payer_account.key) {
                msg!(
                    "Payer {} is not an administrator of tenant {}",
                    payer_account.key,
                    tenant_add_acc.key
                );
                return Err(DoubleZeroError::Unauthorized.into());
            }

            tenant_add.reference_count = tenant_add
                .reference_count
                .checked_add(1)
                .ok_or(DoubleZeroError::InvalidIndex)?;
            try_acc_write(&tenant_add, tenant_add_acc, payer_account, accounts)?;
        }
    }

    // Airdrop rent exempt + configured lamports to user_payer account
    let deposit = Rent::get()
        .unwrap()
        .minimum_balance(AIRDROP_USER_RENT_LAMPORTS_BYTES)
        .saturating_add(globalstate.user_airdrop_lamports)
        .saturating_sub(user_payer.lamports());

    msg!("Airdropping {} lamports to user account", deposit);
    invoke_signed_unchecked(
        &solana_system_interface::instruction::transfer(payer_account.key, user_payer.key, deposit),
        &[
            payer_account.clone(),
            user_payer.clone(),
            system_program.clone(),
        ],
        &[],
    )?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::AIRDROP_USER_RENT_LAMPORTS_BYTES;
    use crate::state::{accounttype::AccountType, user::User};
    use doublezero_program_common::types::NetworkV4;
    use solana_program::pubkey::Pubkey;
    use std::net::Ipv4Addr;

    /// Validates that AIRDROP_USER_RENT_LAMPORTS_BYTES is sufficient to cover
    /// the rent for User accounts with various configurations.
    ///
    /// The constant is sized for 3 User accounts, each with 1 publisher AND 1 subscriber.
    /// This ensures sufficient rent even when supporting simultaneous pub/sub in the future.
    #[test]
    fn test_airdrop_user_rent_lamports_bytes_covers_user_sizes() {
        // User with 1 publisher only (subscriber use case)
        let user_with_publisher = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 1,
            tenant_pk: Pubkey::new_unique(),
            user_type: crate::state::user::UserType::Multicast,
            device_pk: Pubkey::new_unique(),
            cyoa_type: crate::state::user::UserCYOA::GREOverDIA,
            client_ip: Ipv4Addr::new(192, 168, 1, 1),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: NetworkV4::new(Ipv4Addr::new(169, 254, 0, 0), 30).unwrap(),
            status: crate::state::user::UserStatus::Activated,
            publishers: vec![Pubkey::new_unique()],
            subscribers: vec![],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        // User with 1 subscriber only (publisher use case)
        let user_with_subscriber = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 1,
            tenant_pk: Pubkey::new_unique(),
            user_type: crate::state::user::UserType::Multicast,
            device_pk: Pubkey::new_unique(),
            cyoa_type: crate::state::user::UserCYOA::GREOverDIA,
            client_ip: Ipv4Addr::new(192, 168, 1, 1),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: NetworkV4::new(Ipv4Addr::new(169, 254, 0, 0), 30).unwrap(),
            status: crate::state::user::UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        // User with both 1 publisher and 1 subscriber (future simultaneous pub/sub)
        let user_with_both = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 1,
            tenant_pk: Pubkey::new_unique(),
            user_type: crate::state::user::UserType::Multicast,
            device_pk: Pubkey::new_unique(),
            cyoa_type: crate::state::user::UserCYOA::GREOverDIA,
            client_ip: Ipv4Addr::new(192, 168, 1, 1),
            dz_ip: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_id: 100,
            tunnel_net: NetworkV4::new(Ipv4Addr::new(169, 254, 0, 0), 30).unwrap(),
            status: crate::state::user::UserStatus::Activated,
            publishers: vec![Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        let size_with_publisher = borsh::object_length(&user_with_publisher).unwrap();
        let size_with_subscriber = borsh::object_length(&user_with_subscriber).unwrap();
        let size_with_both = borsh::object_length(&user_with_both).unwrap();

        // Verify our understanding of the sizes
        // Base User size (empty vecs) = 176 bytes
        // Each Pubkey in publishers/subscribers adds 32 bytes
        assert_eq!(
            size_with_publisher, 208,
            "User with 1 publisher should be 208 bytes"
        );
        assert_eq!(
            size_with_subscriber, 208,
            "User with 1 subscriber should be 208 bytes"
        );
        assert_eq!(
            size_with_both, 240,
            "User with 1 publisher + 1 subscriber should be 240 bytes"
        );

        // The constant should be sized for 3 accounts with both pub+sub (240 * 3 = 720)
        assert_eq!(
            AIRDROP_USER_RENT_LAMPORTS_BYTES,
            240 * 3,
            "AIRDROP_USER_RENT_LAMPORTS_BYTES should be sized for 3 User accounts with pub+sub"
        );

        // Verify the constant covers at least 3 accounts of the largest expected size
        assert!(
            AIRDROP_USER_RENT_LAMPORTS_BYTES >= size_with_both * 3,
            "AIRDROP_USER_RENT_LAMPORTS_BYTES ({}) must cover 3 User accounts with pub+sub ({})",
            AIRDROP_USER_RENT_LAMPORTS_BYTES,
            size_with_both * 3
        );
    }
}
