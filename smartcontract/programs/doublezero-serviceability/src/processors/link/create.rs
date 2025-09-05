use crate::{
    error::DoubleZeroError,
    globalstate::{globalstate_get_next, globalstate_write},
    helper::*,
    pda::get_link_pda,
    state::{accounttype::AccountType, contributor::Contributor, device::Device, link::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use doublezero_program_common::{types::NetworkV4, validate_account_code};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone, Default)]
pub struct LinkCreateArgs {
    pub code: String,
    pub link_type: LinkLinkType,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay_ns: u64,
    pub jitter_ns: u64,
    pub side_a_iface_name: String,
    pub side_z_iface_name: Option<String>,
}

impl fmt::Debug for LinkCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, link_type: {:?}, bandwidth: {}, mtu: {}, delay_ns: {}, jitter_ns: {}, side_a_iface_name: {}, side_z_iface_name: {:?}",
            self.code, self.link_type, self.bandwidth, self.mtu, self.delay_ns, self.jitter_ns, self.side_a_iface_name, self.side_z_iface_name
        )
    }
}

pub fn process_create_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let side_a_account = next_account_info(accounts_iter)?;
    let side_z_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_link({:?})", value);

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

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

    if !link_account.data.borrow().is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let globalstate = globalstate_get_next(globalstate_account)?;
    let mut contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    let (expected_pda_account, bump_seed) = get_link_pda(program_id, globalstate.account_index);
    assert_eq!(
        link_account.key, &expected_pda_account,
        "Invalid Link PubKey"
    );

    let mut side_a_dev = Device::try_from(side_a_account)?;

    if side_a_dev.contributor_pk != *contributor_account.key {
        return Err(DoubleZeroError::InvalidContributor.into());
    }

    let mut side_z_dev = Device::try_from(side_z_account)?;

    if value.link_type != LinkLinkType::DZX && side_z_dev.contributor_pk != *contributor_account.key
    {
        return Err(DoubleZeroError::InvalidContributor.into());
    }

    if !side_a_dev
        .interfaces
        .iter()
        .any(|iface| iface.into_current_version().name == value.side_a_iface_name)
    {
        #[cfg(test)]
        msg!("{:?}", side_a_dev);

        return Err(DoubleZeroError::InvalidInterfaceName.into());
    }

    let side_z_iface_name = value.side_z_iface_name.clone().unwrap_or_default();
    if value.side_z_iface_name.is_some()
        && !side_z_dev.interfaces.iter().any(|iface| {
            iface.into_current_version().name == value.side_z_iface_name.clone().unwrap()
        })
    {
        #[cfg(test)]
        msg!("{:?}", side_z_dev);

        return Err(DoubleZeroError::InvalidInterfaceName.into());
    }
    if value.link_type == LinkLinkType::DZX && value.side_z_iface_name.is_some() {
        return Err(DoubleZeroError::InvalidInterfaceZForExternal.into());
    }

    let status = if value.link_type == LinkLinkType::DZX {
        LinkStatus::Requested
    } else {
        LinkStatus::Pending
    };

    contributor.reference_count += 1;
    side_a_dev.reference_count += 1;
    side_z_dev.reference_count += 1;

    let tunnel: Link = Link {
        account_type: AccountType::Link,
        owner: *payer_account.key,
        index: globalstate.account_index,
        bump_seed,
        code,
        contributor_pk: *contributor_account.key,
        side_a_pk: *side_a_account.key,
        side_z_pk: *side_z_account.key,
        link_type: value.link_type,
        bandwidth: value.bandwidth,
        mtu: value.mtu,
        delay_ns: value.delay_ns,
        jitter_ns: value.jitter_ns,
        tunnel_id: 0,
        tunnel_net: NetworkV4::default(),
        status,
        side_a_iface_name: value.side_a_iface_name.clone(),
        side_z_iface_name,
    };

    account_create(
        link_account,
        &tunnel,
        payer_account,
        system_program,
        program_id,
    )?;
    account_write(
        contributor_account,
        &contributor,
        payer_account,
        system_program,
    )?;
    account_write(side_a_account, &side_a_dev, payer_account, system_program)?;
    account_write(side_z_account, &side_z_dev, payer_account, system_program)?;
    globalstate_write(globalstate_account, &globalstate)?;

    Ok(())
}
