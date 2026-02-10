use crate::{
    error::DoubleZeroError,
    pda::{get_accesspass_pda, get_user_old_pda, get_user_pda},
    seeds::{SEED_PREFIX, SEED_USER},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::AccountType,
        device::{Device, DeviceStatus},
        globalstate::GlobalState,
        tenant::Tenant,
        user::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    clock::Clock,
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
    sysvar::Sysvar,
};
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct UserCreateArgs {
    pub user_type: UserType,
    pub cyoa_type: UserCYOA,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: std::net::Ipv4Addr,
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub tunnel_endpoint: std::net::Ipv4Addr,
}

impl fmt::Debug for UserCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, client_ip: {}, tunnel_endpoint: {}",
            self.user_type, self.cyoa_type, &self.client_ip, &self.tunnel_endpoint,
        )
    }
}

#[derive(PartialEq)]
enum PDAVersion {
    V1,
    V2,
}

pub fn process_create_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Parse optional tenant account (after globalstate_account)
    let tenant_account = if accounts.len() >= 7 {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    msg!("process_create_user({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate tenant account if provided
    if let Some(tenant_account) = tenant_account {
        // Must not be empty (already initialized)
        if tenant_account.data_is_empty() {
            return Err(DoubleZeroError::InvalidTenantPubkey.into());
        }

        // Must be owned by program
        assert_eq!(
            tenant_account.owner, program_id,
            "Invalid Tenant Account Owner"
        );

        // Must be writable
        assert!(tenant_account.is_writable, "Tenant Account is not writable");

        // Verify account type is Tenant
        if tenant_account.data.borrow()[0] != AccountType::Tenant as u8 {
            return Err(DoubleZeroError::InvalidAccountType.into());
        }
    }

    if !user_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid AccessPass Account Owner"
    );

    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    globalstate.account_index += 1;

    let (expected_old_pda_account, bump_old_seed) =
        get_user_old_pda(program_id, globalstate.account_index);
    let (expected_pda_account, bump_seed) =
        get_user_pda(program_id, &value.client_ip, value.user_type);

    let pda_ver = if user_account.key == &expected_pda_account {
        PDAVersion::V2
    } else if user_account.key == &expected_old_pda_account {
        PDAVersion::V1
    } else {
        msg!(
            "Invalid User PDA. expected: {} or {}, found: {}",
            expected_pda_account,
            expected_old_pda_account,
            user_account.key
        );
        return Err(DoubleZeroError::InvalidUserPubkey.into());
    };

    // Check account Types
    if device_account.data_is_empty()
        || device_account.data.borrow()[0] != AccountType::Device as u8
    {
        return Err(DoubleZeroError::InvalidDevicePubkey.into());
    }
    if device_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &value.client_ip, payer_account.key);
    let (accesspass_dynamic_pda, _) =
        get_accesspass_pda(program_id, &Ipv4Addr::UNSPECIFIED, payer_account.key);
    // Access Pass must exist and match the client_ip or allow_multiple_ip must be enabled
    assert!(
        accesspass_account.key == &accesspass_pda
            || accesspass_account.key == &accesspass_dynamic_pda,
        "Invalid AccessPass PDA",
    );

    // Read Access Pass
    let mut accesspass = AccessPass::try_from(accesspass_account)?;
    if accesspass.user_payer != *payer_account.key {
        msg!(
            "Invalid user_payer accesspass.{{user_payer: {}}} = {{ user_payer: {} }}",
            accesspass.user_payer,
            payer_account.key
        );
        return Err(DoubleZeroError::Unauthorized.into());
    }
    if accesspass.is_dynamic() && accesspass.client_ip == Ipv4Addr::UNSPECIFIED {
        accesspass.client_ip = value.client_ip; // lock to the first used IP
    }
    if !accesspass.allow_multiple_ip() && accesspass.client_ip != value.client_ip {
        msg!(
            "Invalid client_ip accesspass.{{client_ip: {}}} = {{ client_ip: {} }}",
            accesspass.client_ip,
            value.client_ip
        );
        return Err(DoubleZeroError::Unauthorized.into());
    }

    // Enforce tenant_allowlist: if access-pass has a non-default tenant in its
    // allowlist, the user's tenant must be in that list.
    if accesspass
        .tenant_allowlist
        .iter()
        .any(|pk| *pk != Pubkey::default())
    {
        let user_tenant_pk = tenant_account.map(|a| *a.key).unwrap_or(Pubkey::default());
        if !accesspass.tenant_allowlist.contains(&user_tenant_pk) {
            msg!(
                "Tenant {} not in access-pass tenant_allowlist {:?}",
                user_tenant_pk,
                accesspass.tenant_allowlist
            );
            return Err(DoubleZeroError::TenantNotInAccessPassAllowlist.into());
        }
    }

    // Check Initial epoch
    let clock = Clock::get()?;
    let current_epoch = clock.epoch;
    if accesspass.last_access_epoch < current_epoch {
        msg!(
            "Invalid epoch last_access_epoch: {} < current_epoch: {}",
            accesspass.last_access_epoch,
            current_epoch
        );
        return Err(DoubleZeroError::AccessPassUnauthorized.into());
    }

    accesspass.connection_count += 1;
    accesspass.status = AccessPassStatus::Connected;

    // Read validator_pubkey from AccessPass
    let validator_pubkey = match &accesspass.accesspass_type {
        AccessPassType::SolanaValidator(pk) => *pk,
        _ => Pubkey::default(),
    };

    let mut device = Device::try_from(device_account)?;

    let is_qa = globalstate.qa_allowlist.contains(payer_account.key);

    // Only activated devices can have users, or if in foundation allowlist
    if device.status != DeviceStatus::Activated
        && !globalstate.foundation_allowlist.contains(payer_account.key)
        && !is_qa
    {
        msg!("{:?}", device);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    if device.users_count >= device.max_users && !is_qa {
        msg!("{:?}", device);
        return Err(DoubleZeroError::MaxUsersExceeded.into());
    }

    device.reference_count += 1;
    device.users_count += 1;

    // Handle tenant reference counting and get tenant_pk
    let tenant_pk = if let Some(tenant_account) = tenant_account {
        let mut tenant = Tenant::try_from(tenant_account)?;

        tenant.reference_count = tenant
            .reference_count
            .checked_add(1)
            .ok_or(DoubleZeroError::InvalidIndex)?;

        try_acc_write(&tenant, tenant_account, payer_account, accounts)?;

        *tenant_account.key
    } else {
        Pubkey::default()
    };

    let user: User = User {
        account_type: AccountType::User,
        owner: *payer_account.key,
        bump_seed: if pda_ver == PDAVersion::V1 {
            bump_old_seed
        } else {
            bump_seed
        },
        index: if pda_ver == PDAVersion::V1 {
            globalstate.account_index
        } else {
            0
        },
        tenant_pk,
        user_type: value.user_type,
        device_pk: *device_account.key,
        cyoa_type: value.cyoa_type,
        client_ip: value.client_ip,
        dz_ip: std::net::Ipv4Addr::UNSPECIFIED,
        tunnel_id: 0,
        tunnel_net: NetworkV4::default(),
        status: UserStatus::Pending,
        publishers: vec![],
        subscribers: vec![],
        validator_pubkey,
        tunnel_endpoint: value.tunnel_endpoint,
    };

    if pda_ver == PDAVersion::V1 {
        try_acc_create(
            &user,
            user_account,
            payer_account,
            system_program,
            program_id,
            &[
                SEED_PREFIX,
                SEED_USER,
                &user.index.to_le_bytes(),
                &[bump_old_seed],
            ],
        )?;
        try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;
    } else {
        try_acc_create(
            &user,
            user_account,
            payer_account,
            system_program,
            program_id,
            &[
                SEED_PREFIX,
                SEED_USER,
                &user.client_ip.octets(),
                &[user.user_type as u8],
                &[bump_seed],
            ],
        )?
    }

    try_acc_write(&device, device_account, payer_account, accounts)?;
    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

    Ok(())
}
