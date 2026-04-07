use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{
        device::Device,
        user::{BGPStatus, User},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    clock::Clock,
    entrypoint::ProgramResult,
    pubkey::Pubkey,
    sysvar::Sysvar,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct SetUserBGPStatusArgs {
    pub bgp_status: BGPStatus,
}

impl fmt::Debug for SetUserBGPStatusArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "bgp_status: {}", self.bgp_status)
    }
}

pub fn process_set_bgp_status_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetUserBGPStatusArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    assert!(
        payer_account.is_signer,
        "Metrics publisher must be a signer"
    );
    assert!(user_account.is_writable, "User account must be writable");

    assert_eq!(user_account.owner, program_id, "Invalid user account owner");
    assert_eq!(
        device_account.owner, program_id,
        "Invalid device account owner"
    );

    let device = Device::try_from(device_account)?;

    if device.metrics_publisher_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user = User::try_from(user_account)?;

    if user.device_pk != *device_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let slot = Clock::get()?.slot;
    user.bgp_status = value.bgp_status;
    user.last_bgp_reported_at = slot;
    if value.bgp_status == BGPStatus::Up {
        user.last_bgp_up_at = slot;
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    Ok(())
}
