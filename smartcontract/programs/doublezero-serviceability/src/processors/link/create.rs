use core::fmt;

use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get_next, globalstate_write},
    helper::*,
    pda::get_link_pda,
    state::{accounttype::AccountType, contributor::Contributor, device::Device, link::*},
    types::NetworkV4,
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
    pub code: String,
    pub contributor_pk: Pubkey,
    pub side_a_pk: Pubkey,
    pub side_z_pk: Pubkey,
    pub link_type: LinkLinkType,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay_ns: u64,
    pub jitter_ns: u64,
    pub side_a_iface_name: String,
    pub side_z_iface_name: String,
}

impl fmt::Debug for LinkCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, side_a_pk: {}, side_z_pk: {}, link_type: {:?}, bandwidth: {}, mtu: {}, delay_ns: {}, jitter_ns: {}, side_a_iface_name: {}, side_z_iface_name: {}",
            self.code, self.side_a_pk, self.side_z_pk, self.link_type, self.bandwidth, self.mtu, self.delay_ns, self.jitter_ns, self.side_a_iface_name, self.side_z_iface_name
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
    let contributor_account = next_account_info(accounts_iter)?;
    let side_a_account = next_account_info(accounts_iter)?;
    let side_z_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_link({:?})", value);

    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        side_a_account.owner, program_id,
        "Invalid Side A Account Owner"
    );
    assert_eq!(
        side_z_account.owner, program_id,
        "Invalid Side Z Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );

    if !pda_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }
    if globalstate_account.data.borrow().is_empty() {
        return Err(ProgramError::UninitializedAccount);
    }
    let globalstate = globalstate_get_next(globalstate_account)?;

    if !globalstate.device_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (expected_pda_account, bump_seed) = get_link_pda(program_id, globalstate.account_index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid Link PubKey"
    );

    let contributor = Contributor::try_from(contributor_account)?;
    if contributor.account_type != AccountType::Contributor {
        return Err(DoubleZeroError::InvalidContributorPubkey.into());
    }
    if contributor.owner != *payer_account.key {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }
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

    let side_a_dev = Device::try_from(side_a_account)?;
    let side_z_dev = Device::try_from(side_z_account)?;

    if !side_a_dev
        .interfaces
        .iter()
        .any(|iface| iface.name == value.side_a_iface_name)
    {
        return Err(DoubleZeroError::InvalidInterfaceName.into());
    }
    if !side_z_dev
        .interfaces
        .iter()
        .any(|iface| iface.name == value.side_z_iface_name)
    {
        return Err(DoubleZeroError::InvalidInterfaceName.into());
    }

    let tunnel: Link = Link {
        account_type: AccountType::Link,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        code: value.code.clone(),
        contributor_pk: value.contributor_pk,
        side_a_pk: value.side_a_pk,
        side_z_pk: value.side_z_pk,
        link_type: value.link_type,
        bandwidth: value.bandwidth,
        mtu: value.mtu,
        delay_ns: value.delay_ns,
        jitter_ns: value.jitter_ns,
        tunnel_id: 0,
        tunnel_net: NetworkV4::default(),
        status: LinkStatus::Pending,
        side_a_iface_name: value.side_a_iface_name.clone(),
        side_z_iface_name: value.side_z_iface_name.clone(),
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
