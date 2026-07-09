use crate::{
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

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct UserCreateArgs {
    pub user_type: UserType,
    pub cyoa_type: UserCYOA,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: std::net::Ipv4Addr,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub tunnel_endpoint: std::net::Ipv4Addr,
    /// Number of DzPrefixBlock accounts passed for on-chain allocation. Must be > 0:
    /// user creation always allocates resources and activates atomically.
    #[incremental(default = 0)]
    pub dz_prefix_count: u8,
}

impl fmt::Debug for UserCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, client_ip: {}, tunnel_endpoint: {}, dz_prefix_count: {}",
            self.user_type,
            self.cyoa_type,
            &self.client_ip,
            &self.tunnel_endpoint,
            self.dz_prefix_count,
        )
    }
}

pub fn process_create_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserCreateArgs,
) -> ProgramResult {
    if value.dz_prefix_count == 0 {
        msg!("dz_prefix_count must be > 0; CreateUser requires on-chain allocation");
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Required ResourceExtension accounts for on-chain allocation.
    // Account layout:
    //   [user, device, accesspass, globalstate,
    //    user_tunnel_block, multicast_publisher_block, device_tunnel_ids, dz_prefix_0..N,
    //    optional_tenant, payer, system]
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

    // Parse optional tenant account: present iff there is one extra account before payer+system.
    let resource_ext_accounts = 3 + value.dz_prefix_count as usize;
    let tenant_account = if accounts.len() >= 7 + resource_ext_accounts {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    msg!("process_create_user({:?})", value);

    let core_accounts = CreateUserCoreAccounts {
        user_account,
        device_account,
        accesspass_account,
        globalstate_account,
        tenant_account,
        payer_account,
        // CreateUser never overrides the owner (owner_override is None below), so the
        // owner-override authorization that consumes this is never reached.
        permission_account: None,
    };

    let mut result = create_user_core(
        program_id,
        accounts,
        &core_accounts,
        value.user_type,
        value.cyoa_type,
        value.client_ip,
        value.tunnel_endpoint,
        false,
        None,
        // Plain CreateUser is unicast; no multicast group and no feed gate.
        None,
        None,
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
        )?
    }

    try_acc_write(&result.device, device_account, payer_account, accounts)?;
    try_acc_write(
        &result.accesspass,
        accesspass_account,
        payer_account,
        accounts,
    )?;

    Ok(())
}
