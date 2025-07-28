use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get_next, globalstate_write},
    helper::*,
    pda::get_user_pda,
    state::{
        accounttype::AccountType,
        device::{Device, DeviceStatus},
        user::*,
    },
    types::*,
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserCreateArgs {
    pub user_type: UserType,
    pub cyoa_type: UserCYOA,
    pub client_ip: std::net::Ipv4Addr,
}

impl fmt::Debug for UserCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, client_ip: {}",
            self.user_type, self.cyoa_type, &self.client_ip,
        )
    }
}

pub fn process_create_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_user({:?})", value);

    if !pda_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    if globalstate_account.data.borrow().is_empty() {
        return Err(ProgramError::UninitializedAccount);
    }
    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.user_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (expected_pda_account, bump_seed) = get_user_pda(program_id, globalstate.account_index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
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

    let device = Device::try_from(device_account)?;
    if device.status == DeviceStatus::Suspended {
        if !globalstate.foundation_allowlist.contains(payer_account.key) {
            msg!("{:?}", device);
            return Err(DoubleZeroError::InvalidStatus.into());
        }
    } else if device.status != DeviceStatus::Activated {
        msg!("{:?}", device);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let mut device = Device::try_from(device_account)?;
    assert_eq!(device.account_type, AccountType::Device);

    device.reference_count += 1;

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
        publishers: vec![],
        subscribers: vec![],
        validator_pubkey: Pubkey::default(),
    };

    account_create(
        pda_account,
        &user,
        payer_account,
        system_program,
        program_id,
    )?;
    account_write(device_account, &device, payer_account, system_program)?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
