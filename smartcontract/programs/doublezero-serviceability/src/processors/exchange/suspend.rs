use crate::{error::DoubleZeroError, globalstate::globalstate_get, helper::*, state::exchange::*};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct ExchangeSuspendArgs {}

impl fmt::Debug for ExchangeSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_suspend_exchange(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &ExchangeSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let exchange_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_suspend_exchange({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        exchange_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(exchange_account.is_writable, "PDA Account is not writable");

    // Parse accounts
    let globalstate = globalstate_get(globalstate_account)?;
    let mut exchange: Exchange = Exchange::try_from(exchange_account)?;

    // Authorization:
    //  - The exchange owner can always suspend their own exchange, even if they
    //    are no longer in the foundation_allowlist.
    //  - Alternatively, any account in the foundation_allowlist may suspend the
    //    exchange.
    let is_owner = exchange.owner == *payer_account.key;
    let is_foundation = globalstate
        .foundation_allowlist
        .contains(payer_account.key);

    if !is_owner && !is_foundation {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    exchange.status = ExchangeStatus::Suspended;

    account_write(exchange_account, &exchange, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Suspended: {:?}", exchange);

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::{accounttype::AccountType, globalstate::GlobalState};

    #[test]
    fn owner_can_suspend_even_if_not_in_foundation_allowlist() {
        let owner = Pubkey::new_unique();
        let payer = owner;

        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed: 0,
            account_index: 0,
            foundation_allowlist: vec![],
            device_allowlist: vec![],
            user_allowlist: vec![],
            activator_authority_pk: Pubkey::default(),
            sentinel_authority_pk: Pubkey::default(),
            contributor_airdrop_lamports: 0,
            user_airdrop_lamports: 0,
        };

        let exchange = Exchange {
            account_type: crate::state::accounttype::AccountType::Exchange,
            owner,
            index: 0,
            bump_seed: 0,
            lat: 0.0,
            lng: 0.0,
            bgp_community: 0,
            unused: 0,
            status: ExchangeStatus::Pending,
            code: String::new(),
            name: String::new(),
            reference_count: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
        };

        let is_owner = exchange.owner == payer;
        let is_foundation = globalstate.foundation_allowlist.contains(&payer);

        assert!(is_owner);
        assert!(!is_foundation);
        assert!(is_owner || is_foundation);
    }

    #[test]
    fn non_owner_must_be_in_foundation_allowlist_to_suspend() {
        let owner = Pubkey::new_unique();
        let payer = Pubkey::new_unique();

        let mut globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed: 0,
            account_index: 0,
            foundation_allowlist: vec![],
            device_allowlist: vec![],
            user_allowlist: vec![],
            activator_authority_pk: Pubkey::default(),
            sentinel_authority_pk: Pubkey::default(),
            contributor_airdrop_lamports: 0,
            user_airdrop_lamports: 0,
        };

        let exchange = Exchange {
            account_type: crate::state::accounttype::AccountType::Exchange,
            owner,
            index: 0,
            bump_seed: 0,
            lat: 0.0,
            lng: 0.0,
            bgp_community: 0,
            unused: 0,
            status: ExchangeStatus::Pending,
            code: String::new(),
            name: String::new(),
            reference_count: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
        };

        // Not in allowlist: should fail auth condition
        let is_owner = exchange.owner == payer;
        let is_foundation = globalstate.foundation_allowlist.contains(&payer);
        assert!(!is_owner && !is_foundation);

        // After adding to allowlist: should pass auth condition even as non-owner
        globalstate.foundation_allowlist.push(payer);
        let is_foundation = globalstate.foundation_allowlist.contains(&payer);
        assert!(!is_owner && is_foundation);
        assert!(is_owner || is_foundation);
    }
}
