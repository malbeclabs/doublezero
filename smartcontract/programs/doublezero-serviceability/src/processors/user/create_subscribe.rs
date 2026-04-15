use crate::{
    seeds::{SEED_PREFIX, SEED_USER},
    serializer::{try_acc_create, try_acc_write},
    state::{globalstate::GlobalState, user::*},
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
use crate::processors::multicastgroup::subscribe::subscribe_user_to_multicastgroup;

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
    /// Number of DzPrefixBlock accounts passed for on-chain allocation.
    /// When 0, legacy behavior is used (Pending status). When > 0, atomic create+allocate+activate.
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
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let mgroup_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension accounts for on-chain allocation
    // Account layout WITH ResourceExtension (dz_prefix_count > 0):
    //   [user, device, mgroup, accesspass, globalstate, user_tunnel_block, multicast_publisher_block, device_tunnel_ids, dz_prefix_0..N, payer, system]
    // Account layout WITHOUT (legacy, dz_prefix_count == 0):
    //   [user, device, mgroup, accesspass, globalstate, payer, system]
    let resource_extension_accounts = resource_onchain_helpers::parse_resource_extension_accounts(
        accounts_iter,
        value.dz_prefix_count,
    )?;

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    msg!("process_create_subscribe_user({:?})", value);

    let core_accounts = CreateUserCoreAccounts {
        user_account,
        device_account,
        accesspass_account,
        globalstate_account,
        tenant_account: None, // No tenant support for multicast group users
        payer_account,
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
    )?;

    // Subscribe user to multicast group
    let subscribe_result = subscribe_user_to_multicastgroup(
        mgroup_account,
        &result.accesspass,
        &mut result.user,
        value.publisher,
        value.subscriber,
    )?;

    // Atomic create+allocate+activate if on-chain allocation is requested
    if let Some((
        user_tunnel_block_ext,
        multicast_publisher_block_ext,
        device_tunnel_ids_ext,
        dz_prefix_accounts,
    )) = resource_extension_accounts
    {
        let globalstate_ref = GlobalState::try_from(globalstate_account)?;
        resource_onchain_helpers::validate_and_allocate_user_resources(
            program_id,
            &mut result.user,
            user_tunnel_block_ext,
            multicast_publisher_block_ext,
            device_tunnel_ids_ext,
            &dz_prefix_accounts,
            &globalstate_ref,
        )?;

        result.user.try_activate(&mut result.accesspass)?;
    }

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
