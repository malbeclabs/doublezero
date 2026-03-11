use core::fmt;

use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{accounttype::AccountType, device::Device, globalstate::GlobalState, user::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    clock::Clock,
    entrypoint::ProgramResult,
    pubkey::Pubkey,
    sysvar::Sysvar,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct UserSetBGPStatusArgs {
    pub bgp_status: BGPStatus,
}

impl fmt::Debug for UserSetBGPStatusArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "bgp_status: {:?}", self.bgp_status)
    }
}

pub fn process_set_bgp_status_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserSetBGPStatusArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_bgp_status_user({:?})", value);

    assert!(payer_account.is_signer, "Payer must be a signer");

    assert_eq!(user_account.owner, program_id, "Invalid User Account Owner");
    assert_eq!(
        device_account.owner, program_id,
        "Invalid Device Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(user_account.is_writable, "User Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let device = Device::try_from(device_account)?;

    if device.metrics_publisher_pk != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user = User::try_from(user_account)?;

    if user.device_pk != *device_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let current_slot = Clock::get()?.slot;

    user.last_bgp_reported_at = current_slot;
    if value.bgp_status == BGPStatus::Up {
        user.last_bgp_up_at = current_slot;
    }
    user.bgp_status = value.bgp_status;

    try_acc_write(&user, user_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Set BGP status: {:?}", user);

    Ok(())
}
