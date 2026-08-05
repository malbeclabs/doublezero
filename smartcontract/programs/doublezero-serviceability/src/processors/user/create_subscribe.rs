use crate::{
    authorize::split_trailing_permission,
    error::DoubleZeroError,
    seeds::{SEED_PREFIX, SEED_USER},
    serializer::{try_acc_create, try_acc_write},
    state::user::*,
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

use super::{
    create_core::{create_user_core, CreateUserCoreAccounts, PDAVersion},
    resource_onchain_helpers,
};
use crate::{
    processors::{
        feed::{check_feed_metro_coverage, enforce_feed_metro_gate},
        multicastgroup::subscribe::{check_mgroup_allowlists, update_user_multicastgroup_roles},
        validation::validate_program_account,
    },
    state::accesspass::AccessPassType,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct UserCreateSubscribeArgs {
    pub user_type: UserType,
    pub cyoa_type: UserCYOA,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: std::net::Ipv4Addr,
    pub publisher: bool,
    pub subscriber: bool,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub tunnel_endpoint: std::net::Ipv4Addr,
    /// Number of DzPrefixBlock accounts passed for on-chain allocation. Must be > 0:
    /// user creation always allocates resources and activates atomically.
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
    /// Custom owner pubkey. When set (non-default), the payer must be in the foundation allowlist.
    /// The access pass is looked up using this owner instead of the payer.
    #[incremental(default = Pubkey::default())]
    pub owner: Pubkey,
    /// Number of additional writable MulticastGroup accounts following the dz_prefix
    /// blocks (before the optional EdgeSeat feed account). The user is subscribed to the
    /// primary group plus every extra group atomically, before resource allocation and
    /// activation. Old encodings without this byte decode as 0 (single-group behavior).
    #[incremental(default = 0)]
    pub extra_group_count: u8,
}

impl fmt::Debug for UserCreateSubscribeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, client_ip: {}, tunnel_endpoint: {}, dz_prefix_count: {}, owner: {}, extra_group_count: {}",
            self.user_type,
            self.cyoa_type,
            self.client_ip,
            self.tunnel_endpoint,
            self.dz_prefix_count,
            self.owner,
            self.extra_group_count,
        )
    }
}

pub fn process_create_subscribe_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserCreateSubscribeArgs,
) -> ProgramResult {
    if value.dz_prefix_count == 0 {
        msg!("dz_prefix_count must be > 0; CreateSubscribeUser requires on-chain allocation");
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let mgroup_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Required ResourceExtension accounts for on-chain allocation.
    // Account layout:
    //   [user, device, mgroup, accesspass, globalstate,
    //    user_tunnel_block, multicast_publisher_block, device_tunnel_ids, dz_prefix_0..N,
    //    payer, system]
    let (
        user_tunnel_block_ext,
        multicast_publisher_block_ext,
        device_tunnel_ids_ext,
        dz_prefix_accounts,
    ) = resource_onchain_helpers::parse_resource_extension_accounts(
        accounts_iter,
        value.dz_prefix_count,
    )?
    .expect("dz_prefix_count > 0 guarantees Some");

    // Trailing layout after the resource-extension accounts:
    // [mgroup₁..mgroupₙ, feed?, payer, system, permission?]. The extra multicast groups
    // (batch subscription, counted by args.extra_group_count) come first; the optional Feed
    // account (EdgeSeat metro gate — the feed covering the device's exchange and listing the
    // target multicast groups) follows; the optional payer Permission PDA (appended by the SDK
    // when it exists on-chain, authorizing a USER_ADMIN owner-override inside create_user_core)
    // is last. split_trailing_permission identifies the Permission by PDA match rather than by
    // position, so the variable-length regions never confuse it.
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
    let (extra_group_accounts, rest) = leading.split_at(extra_group_count);
    let feed_account = rest.first().copied();

    validate_program_account!(
        mgroup_account,
        program_id,
        writable = true,
        "MulticastGroup"
    );
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

    msg!("process_create_subscribe_user({:?})", value);

    let core_accounts = CreateUserCoreAccounts {
        user_account,
        device_account,
        accesspass_account,
        globalstate_account,
        tenant_account: None, // No tenant support for multicast group users
        payer_account,
        permission_account,
    };

    let owner_override = if value.owner != Pubkey::default() {
        Some(value.owner)
    } else {
        None
    };

    let Some(mut result) = create_user_core(
        program_id,
        accounts,
        &core_accounts,
        value.user_type,
        value.cyoa_type,
        value.client_ip,
        value.tunnel_endpoint,
        value.publisher,
        owner_override,
    )?
    else {
        // A duplicate is an error here, not a no-op: falling through would tick a second feed
        // seat and push a duplicate feed_pks entry that delete would double-release.
        return Err(ProgramError::AccountAlreadyInitialized);
    };

    // EdgeSeat multicast metro gate: the group must be joinable via a feed on the pass serving the
    // device's metro; that feed's seat is ticked and recorded on the User so delete releases it.
    // Feed-gated joins skip the subscriber allowlist; the publisher allowlist always applies, since
    // a feed sells receive only.
    let feed_gated = matches!(
        result.accesspass.accesspass_type,
        AccessPassType::EdgeSeat(_)
    ) && value.user_type == UserType::Multicast;
    if feed_gated {
        let feed_key = enforce_feed_metro_gate(
            program_id,
            &mut result.accesspass,
            &result.device.exchange_pk,
            Some(mgroup_account.key),
            feed_account,
        )?;
        result.user.feed_pks.push(feed_key);
    }
    check_mgroup_allowlists(
        &result.accesspass,
        mgroup_account.key,
        value.publisher,
        value.subscriber && !feed_gated,
    )?;

    // Subscribe user to the primary multicast group; the feed metro gate above already
    // covered it (and ticked the seat) for EdgeSeat passes.
    let subscribe_result = update_user_multicastgroup_roles(
        mgroup_account,
        &mut result.user,
        value.publisher,
        value.subscriber,
    )?;

    // Subscribe the extra groups, each authorized exactly like the primary: the
    // publisher allowlist always applies, and the subscriber role comes from the
    // allowlist unless the feed metro gate covers it — extra groups on EdgeSeat
    // passes get a coverage-only check against the same feed (a feed carries a
    // group set; the seat is per-user-per-feed and was ticked once by the gate
    // above). Any failure aborts the whole instruction: no user account,
    // counters, or seat tick survive a partial batch.
    for extra_group_account in extra_group_accounts {
        validate_program_account!(
            *extra_group_account,
            program_id,
            writable = true,
            "MulticastGroup"
        );
        if feed_gated {
            check_feed_metro_coverage(
                program_id,
                &result.accesspass,
                &result.device.exchange_pk,
                Some(extra_group_account.key),
                feed_account,
            )?;
        }
        check_mgroup_allowlists(
            &result.accesspass,
            extra_group_account.key,
            value.publisher,
            value.subscriber && !feed_gated,
        )?;
        let extra_result = update_user_multicastgroup_roles(
            extra_group_account,
            &mut result.user,
            value.publisher,
            value.subscriber,
        )?;
        try_acc_write(
            &extra_result.mgroup,
            extra_group_account,
            payer_account,
            accounts,
        )?;
    }

    // Always allocate resources and activate atomically.
    resource_onchain_helpers::validate_and_allocate_user_resources(
        program_id,
        &mut result.user,
        user_tunnel_block_ext,
        multicast_publisher_block_ext,
        device_tunnel_ids_ext,
        &dz_prefix_accounts,
    )?;

    result.user.try_activate(&mut result.accesspass)?;

    if result.pda_ver == PDAVersion::V1 {
        try_acc_create(
            &result.user,
            user_account,
            payer_account,
            system_program,
            program_id,
            &[
                SEED_PREFIX,
                SEED_USER,
                &result.user.index.to_le_bytes(),
                &[result.bump_old_seed],
            ],
        )?;
        try_acc_write(
            &result.globalstate,
            globalstate_account,
            payer_account,
            accounts,
        )?;
    } else {
        try_acc_create(
            &result.user,
            user_account,
            payer_account,
            system_program,
            program_id,
            &[
                SEED_PREFIX,
                SEED_USER,
                &result.user.client_ip.octets(),
                &[result.user.user_type as u8],
                &[result.bump_seed],
            ],
        )?;
    }

    try_acc_write(
        &subscribe_result.mgroup,
        mgroup_account,
        payer_account,
        accounts,
    )?;
    try_acc_write(&result.device, device_account, payer_account, accounts)?;
    try_acc_write(
        &result.accesspass,
        accesspass_account,
        payer_account,
        accounts,
    )?;

    Ok(())
}
