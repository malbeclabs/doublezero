use crate::{
    error::DoubleZeroError,
    format_option,
    helper::format_option_displayable,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, tenant::Tenant, user::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct UserUpdateArgs {
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub dz_ip: Option<std::net::Ipv4Addr>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
    pub validator_pubkey: Option<Pubkey>,
    pub tenant_pk: Option<Pubkey>,
}

impl fmt::Debug for UserUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, dz_ip: {}, tunnel_id: {}, tunnel_net: {}, validator_pubkey: {}, tenant_pk: {}",
            format_option!(self.user_type),
            format_option!(self.cyoa_type),
            format_option!(self.dz_ip),
            format_option!(self.tunnel_id),
            format_option!(self.tunnel_net),
            format_option!(self.validator_pubkey),
            format_option!(self.tenant_pk),
        )
    }
}

pub fn process_update_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    // Check if tenant accounts are provided (new format with tenant reference counting)
    // Old format: 4 accounts (user, globalstate, payer, system_program)
    // New format: 6 accounts (user, globalstate, payer, system_program, old_tenant, new_tenant)
    let has_tenant_accounts = accounts.len() >= 6;
    let old_tenant_account = if has_tenant_accounts {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };
    let new_tenant_account = if has_tenant_accounts {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    #[cfg(test)]
    msg!("process_update_user({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(user_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::try_from(user_account)?;

    if let Some(value) = value.dz_ip {
        user.dz_ip = value;
    }
    if let Some(value) = value.tunnel_id {
        user.tunnel_id = value;
    }
    if let Some(value) = value.tunnel_net {
        user.tunnel_net = value;
    }
    if let Some(value) = value.user_type {
        user.user_type = value;
    }
    if let Some(value) = value.cyoa_type {
        user.cyoa_type = value;
    }
    if let Some(value) = value.validator_pubkey {
        user.validator_pubkey = value;
    }
    if let Some(new_tenant_pk) = value.tenant_pk {
        // If tenant accounts are provided, update reference counts
        if let (Some(old_tenant_acc), Some(new_tenant_acc)) =
            (old_tenant_account, new_tenant_account)
        {
            // Validate old tenant matches current user tenant
            assert_eq!(
                old_tenant_acc.key, &user.tenant_pk,
                "Old tenant account doesn't match current user tenant"
            );

            // Validate new tenant matches the requested tenant
            assert_eq!(
                new_tenant_acc.key, &new_tenant_pk,
                "New tenant account doesn't match requested tenant"
            );

            // Check account ownership
            assert_eq!(
                old_tenant_acc.owner, program_id,
                "Invalid Old Tenant Account Owner"
            );
            assert_eq!(
                new_tenant_acc.owner, program_id,
                "Invalid New Tenant Account Owner"
            );

            // Check writability
            assert!(
                old_tenant_acc.is_writable,
                "Old Tenant Account is not writable"
            );
            assert!(
                new_tenant_acc.is_writable,
                "New Tenant Account is not writable"
            );

            // Update reference counts
            let mut old_tenant = Tenant::try_from(old_tenant_acc)?;
            let mut new_tenant = Tenant::try_from(new_tenant_acc)?;

            // Decrement old tenant reference count
            old_tenant.reference_count = old_tenant
                .reference_count
                .checked_sub(1)
                .ok_or(DoubleZeroError::InvalidIndex)?;

            // Increment new tenant reference count
            new_tenant.reference_count = new_tenant
                .reference_count
                .checked_add(1)
                .ok_or(DoubleZeroError::InvalidIndex)?;

            // Write updated tenants
            try_acc_write(&old_tenant, old_tenant_acc, payer_account, accounts)?;
            try_acc_write(&new_tenant, new_tenant_acc, payer_account, accounts)?;
        }

        user.tenant_pk = new_tenant_pk;
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", user);

    Ok(())
}
