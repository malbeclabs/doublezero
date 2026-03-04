use crate::{
    error::DoubleZeroError,
    helper::assign_bgp_community,
    pda::get_globalconfig_pda,
    serializer::try_acc_write,
    state::{exchange::Exchange, globalconfig::GlobalConfig, globalstate::GlobalState},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::validate_account_code;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct ExchangeUpdateArgs {
    pub code: Option<String>,
    pub name: Option<String>,
    pub lat: Option<f64>,
    pub lng: Option<f64>,
    pub bgp_community: Option<u16>,
}

impl fmt::Debug for ExchangeUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, name: {:?}, lat: {:?}, lng: {:?}, bgp_community: {:?}",
            self.code, self.name, self.lat, self.lng, self.bgp_community
        )
    }
}

pub fn process_update_exchange(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ExchangeUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let exchange_account = next_account_info(accounts_iter)?;
    let globalconfig_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_exchange({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        exchange_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalconfig_account.owner, program_id,
        "Invalid GlobalConfig Account Owner"
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
    // Check if the account is writable
    assert!(exchange_account.is_writable, "PDA Account is not writable");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // We need to access globalconfig in order to assign BGP community
    let mut globalconfig = GlobalConfig::try_from(globalconfig_account)?;
    let (globalconfig_pda, _) = get_globalconfig_pda(program_id);
    assert_eq!(
        globalconfig_account.key, &globalconfig_pda,
        "Invalid GlobalConfig PubKey"
    );

    let mut exchange: Exchange = Exchange::try_from(exchange_account)?;

    if let Some(ref code) = value.code {
        exchange.code =
            validate_account_code(code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
    }
    if let Some(ref name) = value.name {
        exchange.name = name.clone();
    }
    if let Some(ref lat) = value.lat {
        exchange.lat = *lat;
    }
    if let Some(ref lng) = value.lng {
        exchange.lng = *lng;
    }
    if let Some(_bgp_community) = value.bgp_community {
        exchange.bgp_community = assign_bgp_community(&mut globalconfig);
        try_acc_write(&globalconfig, globalconfig_account, payer_account, accounts)?;
    }

    try_acc_write(&exchange, exchange_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", exchange);

    Ok(())
}
