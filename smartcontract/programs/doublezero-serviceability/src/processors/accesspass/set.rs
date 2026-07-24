use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::*,
    processors::accesspass::airdrop_user_credits,
    seeds::{SEED_ACCESS_PASS, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType, ALLOW_MULTIPLE_IP},
        accounttype::AccountType,
        globalstate::GlobalState,
        permission::permission_flags,
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
    pubkey::Pubkey,
    sysvar::Sysvar,
};

use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct SetAccessPassArgs {
    pub accesspass_type: AccessPassType, // 1 or 33
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr, // 4
    pub last_access_epoch: u64,          // 8
    pub allow_multiple_ip: bool,         // 1
    #[incremental(default = 1)]
    pub max_unicast_users: u16, // 2
    #[incremental(default = 1)]
    pub max_multicast_users: u16, // 2
}

impl fmt::Debug for SetAccessPassArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "accesspass_type: {}, ip: {}, last_access_epoch: {}, allow_multiple_ip: {}, max_unicast_users: {}, max_multicast_users: {}",
            self.accesspass_type,
            self.client_ip,
            self.last_access_epoch,
            self.allow_multiple_ip,
            self.max_unicast_users,
            self.max_multicast_users,
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

    // Optional tenant accounts for reference counting (backwards compatible).
    //
    // The account layout is a fixed prefix `[accesspass, globalstate, user_payer]`,
    // an optional `[tenant_remove, tenant_add]` pair, the fixed `[payer, system]`,
    // and an optional trailing Permission account appended by the SDK for
    // `authorize()`. That yields exactly four possible lengths:
    //   5 = no tenant, no permission        6 = no tenant, permission
    //   7 = tenant,   no permission         8 = tenant,   permission
    // so `>= 7` unambiguously selects the tenant-present shapes. The trailing
    // Permission account never collides with this check because it is read last
    // by `authorize()`, which independently verifies its PDA address, program
    // ownership, and `AccountType::Permission` discriminator — a misclassified
    // account can never be accepted as either a tenant or a permission account.
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

    // Parse the global state account & resolve authorization. A caller is allowed if either:
    //   - they pass the ACCESS_PASS_ADMIN permission check (foundation allowlist, sentinel
    //     authority, feed authority, or a Permission account granting ACCESS_PASS_ADMIN), or
    //   - they are an administrator of the tenant being added (tenant_add_account).
    let globalstate = GlobalState::try_from(globalstate_account)?;

    // Pre-deserialize the tenant_add account when present so we can both authorize the caller
    // and later increment its reference_count without double-reading it.
    let tenant_add_pre = match tenant_add_account {
        Some(acc) if acc.key != &Pubkey::default() => {
            assert_eq!(*acc.owner, *program_id, "Invalid Tenant Add Account Owner");
            Some(Tenant::try_from(acc)?)
        }
        _ => None,
    };

    let is_tenant_admin = tenant_add_pre
        .as_ref()
        .map(|t| t.administrators.contains(payer_account.key))
        .unwrap_or(false);

    // A caller is "privileged" when they pass the ACCESS_PASS_ADMIN permission check
    // (foundation allowlist, sentinel authority, feed authority, or a Permission account
    // granting ACCESS_PASS_ADMIN). Privileged callers retain unrestricted authority for the
    // tenant_remove path below; a tenant administrator is only authorized for their own tenant.
    let is_privileged = authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::ACCESS_PASS_ADMIN,
    )
    .is_ok();

    if !is_privileged && !is_tenant_admin {
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
            unicast_user_count: 0,
            max_unicast_users: value.max_unicast_users,
            multicast_user_count: 0,
            max_multicast_users: value.max_multicast_users,
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

            let ap = AccessPass::try_from(accesspass_account)?;

            // Feed authority can only update access passes they own
            if globalstate.feed_authority_pk == *payer_account.key && ap.owner != *payer_account.key
            {
                msg!("Feed authority can only update access passes they own");
                return Err(DoubleZeroError::NotAllowed.into());
            }

            ap
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
                unicast_user_count: 0,
                max_unicast_users: value.max_unicast_users,
                multicast_user_count: 0,
                max_multicast_users: value.max_multicast_users,
            }
        };

        // Update fields. The max caps are overwritten from args; the live counts are left
        // untouched so an in-flight pass keeps its current seat usage.
        //
        // EdgeSeat feed seats are owned by SetAccessPassFeeds (the oracle), not this instruction.
        // SetAccessPassArgs carries no feed payload, so when both the stored and incoming types are
        // EdgeSeat we preserve the provisioned seat vector instead of clobbering it (and its live
        // current_users) with the incoming empty vec.
        accesspass.accesspass_type = match (&accesspass.accesspass_type, &value.accesspass_type) {
            (AccessPassType::EdgeSeat(existing), AccessPassType::EdgeSeat(_)) => {
                AccessPassType::EdgeSeat(existing.clone())
            }
            _ => value.accesspass_type.clone(),
        };
        accesspass.last_access_epoch = value.last_access_epoch;
        accesspass.flags = flags;
        accesspass.max_unicast_users = value.max_unicast_users;
        accesspass.max_multicast_users = value.max_multicast_users;

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
            assert_eq!(
                *tenant_remove_acc.owner, *program_id,
                "Invalid Tenant Remove Account Owner"
            );
            assert!(
                tenant_remove_acc.is_writable,
                "Tenant Remove Account is not writable"
            );
            let mut tenant_remove = Tenant::try_from(tenant_remove_acc)?;

            // Non-privileged callers may only remove a tenant they administer. Privileged
            // callers (sentinel/feed/foundation) retain unrestricted removal authority to
            // preserve prior behavior.
            if !is_privileged && !tenant_remove.administrators.contains(payer_account.key) {
                msg!(
                    "Payer {} is not an administrator of tenant {} being removed",
                    payer_account.key,
                    tenant_remove_acc.key
                );
                return Err(DoubleZeroError::NotAllowed.into());
            }

            tenant_remove.reference_count = tenant_remove.reference_count.saturating_sub(1);
            try_acc_write(&tenant_remove, tenant_remove_acc, payer_account, accounts)?;
        }
    }

    if let Some(tenant_add_acc) = tenant_add_account {
        if tenant_add_acc.key != &Pubkey::default() {
            // We deserialized the tenant up front for authorization. Take it back here
            // so we can mutate and persist the updated reference_count.
            let mut tenant_add =
                tenant_add_pre.expect("tenant_add_pre is Some when tenant_add_acc is non-default");
            assert!(
                tenant_add_acc.is_writable,
                "Tenant Add Account is not writable"
            );

            // Foundation/sentinel/feed callers must still administer the tenant they add,
            // matching the prior behavior.
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

    // Airdrop rent exempt + configured lamports to user_payer account. For passes that allow
    // multiple IPs (e.g. seat keypairs that provision many nodes), scale the target by the sum of
    // the per-category caps so the keypair holds enough SOL to pay for every create_user it admits.
    // Passes without the flag keep today's fixed (1x) airdrop.
    let multiplier = if value.allow_multiple_ip {
        (value.max_unicast_users as u64).saturating_add(value.max_multicast_users as u64)
    } else {
        1
    };
    airdrop_user_credits(
        payer_account,
        user_payer,
        system_program,
        globalstate.user_airdrop_lamports,
        multiplier,
    )?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use crate::{
        processors::accesspass::AIRDROP_USER_RENT_LAMPORTS_BYTES,
        state::{accounttype::AccountType, user::User},
    };
    use doublezero_program_common::types::NetworkV4;
    use solana_program::pubkey::Pubkey;
    use std::net::Ipv4Addr;

    /// Validates that AIRDROP_USER_RENT_LAMPORTS_BYTES is sufficient to cover
    /// the rent for User accounts with various configurations.
    ///
    /// The constant is sized for 3 User accounts, each with 1 publisher AND 1 subscriber AND
    /// 1 feed seat — the largest single create: an EdgeSeat `CreateSubscribeUser` with both
    /// roles also ticks a feed and records it in `feed_pks`.
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
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
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
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![],
        };

        // User with 1 publisher, 1 subscriber, and 1 feed seat (EdgeSeat both-roles create)
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
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            feed_pks: vec![Pubkey::new_unique()],
        };

        let size_with_publisher = borsh::object_length(&user_with_publisher).unwrap();
        let size_with_subscriber = borsh::object_length(&user_with_subscriber).unwrap();
        let size_with_both = borsh::object_length(&user_with_both).unwrap();

        // Verify our understanding of the sizes
        // Base User size (empty vecs) = 206 bytes (includes tunnel_flags, bgp_status, last_bgp_up_at,
        // last_bgp_reported_at, bgp_rtt_ns, and the 4-byte empty feed_pks vec length prefix)
        // Each Pubkey in publishers/subscribers/feed_pks adds 32 bytes
        assert_eq!(
            size_with_publisher, 238,
            "User with 1 publisher should be 238 bytes"
        );
        assert_eq!(
            size_with_subscriber, 238,
            "User with 1 subscriber should be 238 bytes"
        );
        assert_eq!(
            size_with_both, 302,
            "User with 1 publisher + 1 subscriber + 1 feed should be 302 bytes"
        );

        // The constant should be sized for 3 accounts with pub+sub+feed (302 * 3 = 906)
        assert_eq!(
            AIRDROP_USER_RENT_LAMPORTS_BYTES,
            302 * 3,
            "AIRDROP_USER_RENT_LAMPORTS_BYTES should be sized for 3 User accounts with pub+sub+feed"
        );

        // Verify the constant covers at least 3 accounts of the largest expected size
        assert!(
            AIRDROP_USER_RENT_LAMPORTS_BYTES >= size_with_both * 3,
            "AIRDROP_USER_RENT_LAMPORTS_BYTES ({}) must cover 3 User accounts with pub+sub+feed ({})",
            AIRDROP_USER_RENT_LAMPORTS_BYTES,
            size_with_both * 3
        );
    }
}
