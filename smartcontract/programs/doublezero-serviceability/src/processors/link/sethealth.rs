use crate::{
    authorize::authorize,
    processors::validation::validate_program_account,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, link::*, permission::permission_flags},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkSetHealthArgs {
    pub health: LinkHealth,
}

impl fmt::Debug for LinkSetHealthArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "health: {:?}", self.health)
    }
}

pub fn process_set_health_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkSetHealthArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_health_link({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(link_account, program_id, writable = true, "Link");
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        "GlobalState"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;

    // Authorization: HEALTH_ORACLE or foundation, via a Permission account or the
    // legacy health_oracle_pk / foundation_allowlist (HEALTH_ORACLE covers the
    // oracle key, NETWORK_ADMIN covers foundation).
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::HEALTH_ORACLE | permission_flags::NETWORK_ADMIN,
    )?;

    let mut link: Link = Link::try_from(link_account)?;

    link.link_health = value.health;

    link.check_status_transition();

    try_acc_write(&link, link_account, payer_account, accounts)?;

    msg!("Set Health: {:?}", link);

    Ok(())
}
