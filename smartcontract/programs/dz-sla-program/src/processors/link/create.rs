use core::fmt;

use crate::globalstate::{globalstate_get_next, globalstate_write};
use crate::pda::*;
use crate::{
    error::DoubleZeroError,
    helper::*,
    state::{accounttype::AccountType, link::*},
};
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
pub struct LinkCreateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub code: String,
    pub side_a_pk: Pubkey,
    pub side_z_pk: Pubkey,
    pub link_type: LinkLinkType,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay_ns: u64,
    pub jitter_ns: u64,
}

impl fmt::Debug for LinkCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, side_a_pk: {}, side_z_pk: {}, link_type: {:?}, bandwidth: {}, mtu: {}, delay_ns: {}, jitter_ns: {}",
            self.code, self.side_a_pk, self.side_z_pk, self.link_type, self.bandwidth, self.mtu, self.delay_ns, self.jitter_ns
        )
    }
}

pub fn process_create_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let side_a_account = next_account_info(accounts_iter)?;
    let side_z_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_link({:?})", value);

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

    if !globalstate.device_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (expected_pda_account, bump_seed) = get_link_pda(program_id, globalstate.account_index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid Location PubKey"
    );
    assert!(bump_seed == value.bump_seed, "Invalid Location Bump Seed");

    // Check account Types
    if side_a_account.data_is_empty()
        || side_a_account.data.borrow()[0] != AccountType::Device as u8
    {
        return Err(DoubleZeroError::InvalidDeviceAPubkey.into());
    }
    if side_z_account.data_is_empty()
        || side_z_account.data.borrow()[0] != AccountType::Device as u8
    {
        return Err(DoubleZeroError::InvalidDeviceZPubkey.into());
    }

    let tunnel: Link = Link {
        account_type: AccountType::Link,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        code: value.code.clone(),
        side_a_pk: value.side_a_pk,
        side_z_pk: value.side_z_pk,
        link_type: value.link_type,
        bandwidth: value.bandwidth,
        mtu: value.mtu,
        delay_ns: value.delay_ns,
        jitter_ns: value.jitter_ns,
        tunnel_id: 0,
        tunnel_net: ([0, 0, 0, 0], 0),
        status: LinkStatus::Pending,
    };

    account_create(
        pda_account,
        &tunnel,
        payer_account,
        system_program,
        program_id,
    )?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
