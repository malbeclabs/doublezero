use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get_next, globalstate_write},
    helper::*,
    pda::{get_accesspass_pda, get_user_pda},
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::{AccountType, AccountTypeInfo},
        device::{Device, DeviceStatus},
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
        user::*,
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use doublezero_program_common::{resize_account::resize_account_if_needed, types::NetworkV4};
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

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserCreateSubscribeArgs {
    pub user_type: UserType,
    pub cyoa_type: UserCYOA,
    pub client_ip: std::net::Ipv4Addr,
    pub publisher: bool,
    pub subscriber: bool,
}

impl fmt::Debug for UserCreateSubscribeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, client_ip: {}",
            self.user_type, self.cyoa_type, &self.client_ip,
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
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_user({:?})", value);

    if !user_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid AccessPass Account Owner"
    );

    let globalstate = globalstate_get_next(globalstate_account)?;

    let (expected_pda_account, bump_seed) = get_user_pda(program_id, globalstate.account_index);
    assert_eq!(
        user_account.key, &expected_pda_account,
        "Invalid User PubKey"
    );

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

    // Invalid Access Pass
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::Unauthorized.into());
    }

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
    if accesspass.is_dynamic() && accesspass.client_ip == Ipv4Addr::UNSPECIFIED {
        accesspass.client_ip = value.client_ip; // lock to the first used IP
    }

    // Read validator_pubkey from AccesPass
    let validator_pubkey = match accesspass.accesspass_type {
        AccessPassType::SolanaValidator(pk) => pk,
        AccessPassType::Prepaid => Pubkey::default(),
    };

    let mut mgroup: MulticastGroup = MulticastGroup::try_from(mgroup_account)?;
    assert_eq!(mgroup.status, MulticastGroupStatus::Activated);

    // Check if the user is in the allowlist
    if value.publisher && !accesspass.mgroup_pub_allowlist.contains(mgroup_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if value.subscriber && !accesspass.mgroup_sub_allowlist.contains(mgroup_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device = Device::try_from(device_account)?;

    if device.status == DeviceStatus::Suspended {
        if !globalstate.foundation_allowlist.contains(payer_account.key) {
            msg!("{:?}", device);
            return Err(DoubleZeroError::InvalidStatus.into());
        }
    } else if device.status != DeviceStatus::Activated {
        msg!("{:?}", device);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    if device.users_count >= device.max_users {
        msg!("{:?}", device);
        return Err(DoubleZeroError::MaxUsersExceeded.into());
    }

    device.reference_count += 1;
    device.users_count += 1;

    let user: User = User {
        account_type: AccountType::User,
        owner: *payer_account.key,
        bump_seed,
        index: globalstate.account_index,
        tenant_pk: Pubkey::default(),
        user_type: value.user_type,
        device_pk: *device_account.key,
        cyoa_type: value.cyoa_type,
        client_ip: value.client_ip,
        dz_ip: std::net::Ipv4Addr::UNSPECIFIED,
        tunnel_id: 0,
        tunnel_net: NetworkV4::default(),
        status: UserStatus::Pending,
        publishers: match value.publisher {
            true => vec![*mgroup_account.key],
            false => vec![],
        },
        subscribers: match value.subscriber {
            true => vec![*mgroup_account.key],
            false => vec![],
        },
        validator_pubkey,
    };

    // Update multicastgroup counts
    if value.publisher {
        mgroup.publisher_count = mgroup.publisher_count.saturating_add(1);
    }
    if value.subscriber {
        mgroup.subscriber_count = mgroup.subscriber_count.saturating_add(1);
    }

    account_write(mgroup_account, &mgroup, payer_account, system_program)?;
    account_create(
        user_account,
        &user,
        payer_account,
        system_program,
        program_id,
    )?;
    account_write(device_account, &device, payer_account, system_program)?;
    globalstate_write(globalstate_account, &globalstate)?;
    resize_account_if_needed(
        accesspass_account,
        payer_account,
        accounts,
        accesspass.size(),
    )?;
    accesspass.try_serialize(accesspass_account)?;

    Ok(())
}
