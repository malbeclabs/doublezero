use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get_next, globalstate_write},
    helper::*,
    pda::*,
    state::{
        accounttype::AccountType,
        multicastgroup::{MulticastGroup, MulticastGroupStatus},
        user::*,
    },
    types::*,
};
use core::fmt;

use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserCreateSubscribeArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub user_type: UserType,
    pub device_pk: Pubkey,
    pub cyoa_type: UserCYOA,
    pub client_ip: IpV4,
    pub publisher: bool,
    pub subscriber: bool,
}

impl fmt::Debug for UserCreateSubscribeArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, device_pk: {}, cyoa_type: {}, client_ip: {}",
            self.user_type,
            self.device_pk,
            self.cyoa_type,
            ipv4_to_string(&self.client_ip)
        )
    }
}

pub fn process_create_subscribe_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserCreateSubscribeArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let mgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_user({:?})", value);

    if !pda_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    if globalstate_account.data.borrow().is_empty() {
        panic!("GlobalState account not initialized");
    }
    let globalstate = globalstate_get_next(globalstate_account)?;
    assert_eq!(
        value.index, globalstate.account_index,
        "Invalid Value Index"
    );

    if !globalstate.user_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (expected_pda_account, bump_seed) = get_user_pda(program_id, globalstate.account_index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid User PubKey"
    );
    assert_eq!(bump_seed, value.bump_seed, "Invalid User Bump Seed");

    // Check account Types
    if device_account.data_is_empty()
        || device_account.data.borrow()[0] != AccountType::Device as u8
    {
        panic!("Invalid Device Pubkey");
    }
    if device_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut mgroup: MulticastGroup = MulticastGroup::try_from(mgroup_account)?;
    assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
    assert_eq!(mgroup.status, MulticastGroupStatus::Activated);

    // Check if the user is in the allowlist
    if value.publisher && !mgroup.pub_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }
    if value.subscriber && !mgroup.sub_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let user: User = User {
        account_type: AccountType::User,
        owner: *payer_account.key,
        bump_seed,
        index: globalstate.account_index,
        tenant_pk: Pubkey::default(),
        user_type: value.user_type,
        device_pk: value.device_pk,
        cyoa_type: value.cyoa_type,
        client_ip: value.client_ip,
        dz_ip: [0, 0, 0, 0],
        tunnel_id: 0,
        tunnel_net: ([0, 0, 0, 0], 0),
        status: UserStatus::Pending,
        publishers: match value.publisher {
            true => vec![*mgroup_account.key],
            false => vec![],
        },
        subscribers: match value.subscriber {
            true => vec![*mgroup_account.key],
            false => vec![],
        },
    };

    if value.publisher && !mgroup.publishers.contains(pda_account.key) {
        mgroup.publishers.push(*pda_account.key);
    }
    if value.subscriber && !mgroup.subscribers.contains(pda_account.key) {
        mgroup.subscribers.push(*pda_account.key);
    }

    account_write(mgroup_account, &mgroup, payer_account, system_program);
    account_create(
        pda_account,
        &user,
        payer_account,
        system_program,
        program_id,
    )?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
