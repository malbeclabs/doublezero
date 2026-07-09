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
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

use super::{
    create_core::{create_user_core, CreateUserCoreAccounts, PDAVersion},
    resource_onchain_helpers,
};
use crate::processors::multicastgroup::subscribe::update_user_multicastgroup_roles;

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
}

impl fmt::Debug for UserCreateSubscribeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, client_ip: {}, tunnel_endpoint: {}, dz_prefix_count: {}, owner: {}",
            self.user_type,
            self.cyoa_type,
            &self.client_ip,
            &self.tunnel_endpoint,
            self.dz_prefix_count,
            self.owner,
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

    // Trailing layout after the resource-extension accounts: [feed?, payer, system, permission?].
    // The optional Feed account (EdgeSeat metro gate — the feed covering the device's exchange and
    // listing the target multicast group) precedes payer/system; the optional payer Permission PDA
    // (appended by the SDK when it exists on-chain, authorizing a USER_ADMIN owner-override inside
    // create_user_core) is last. split_trailing_permission identifies the Permission by PDA match
    // rather than by position, so Feed and Permission coexist unambiguously — a single positional
    // slot cannot, since either may be present or absent independently.
    let remaining: Vec<&AccountInfo> = accounts_iter.collect();
    let (payer_account, system_program, leading, permission_account) =
        split_trailing_permission(program_id, &remaining)?;
    let feed_account = leading.first().copied();

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

    let mut result = create_user_core(
        program_id,
        accounts,
        &core_accounts,
        value.user_type,
        value.cyoa_type,
        value.client_ip,
        value.tunnel_endpoint,
        value.publisher,
        owner_override,
        Some(mgroup_account.key),
        feed_account,
    )?;

    // Subscribe user to multicast group
    let subscribe_result = update_user_multicastgroup_roles(
        mgroup_account,
        &result.accesspass,
        &mut result.user,
        value.publisher,
        value.subscriber,
    )?;

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
