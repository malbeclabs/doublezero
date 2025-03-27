use core::fmt;

use crate::error::DoubleZeroError;
use crate::helper::*;
use crate::pda::*;
use crate::state::{accounttype::AccountType, user::*};
use crate::types::*;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
#[cfg(test)]
use solana_program::msg;


#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserCreateArgs {
    pub index: u128,
    pub user_type: UserType,
    pub device_pk: Pubkey,
    pub cyoa_type: UserCYOA,
    pub client_ip: IpV4,
}

impl fmt::Debug for UserCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "user_type: {}, device_pk: {}, cyoa_type: {}, client_ip: {}", self.user_type, self.device_pk, self.cyoa_type, ipv4_to_string(&self.client_ip))
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
        panic!("GlobalState account not initialized");
    }
    let globalstate = globalstate_get_next(globalstate_account)?;
    assert_eq!(value.index, globalstate.account_index, "Invalid Value Index");  

    if !globalstate.user_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (expected_pda_account, bump_seed) = get_user_pda(program_id, globalstate.account_index);
    assert_eq!(pda_account.key, &expected_pda_account,"Invalid User PubKey");

    // Check account Types
    if device_account.data_is_empty() || device_account.data.borrow()[0] != AccountType::Device as u8 {
        panic!("Invalid Device Pubkey");
    }
    if device_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }     

    let user: User = User {
        account_type: AccountType::User,
        owner: *payer_account.key,
        index: globalstate.account_index,
        tenant_pk: Pubkey::default(),
        user_type: value.user_type,
        device_pk: value.device_pk,
        cyoa_type: value.cyoa_type,
        client_ip: value.client_ip,
        dz_ip: [0,0,0,0],
        tunnel_id: 0,
        tunnel_net: ([0, 0, 0, 0], 0),
        status: UserStatus::Pending,
    };

    account_create(
        pda_account,
        &user,
        payer_account,
        system_program,
        program_id,
        bump_seed,
    )?;
    globalstate_write(globalstate_account, &globalstate)?;
    
    Ok(())
}

