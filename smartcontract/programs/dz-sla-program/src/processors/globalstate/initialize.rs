use crate::addresses::doublezero_foundation;
use crate::helper::globalstate_get;
use crate::pda::*;
use crate::processors::globalstate::foundation_allowlist::add::{process_add_foundation_allowlist_globalconfig, AddFoundationAllowlistGlobalConfigArgs};
use crate::seeds::{SEED_GLOBALSTATE, SEED_PREFIX};
use crate::state::accounttype::AccountType;
use crate::state::globalstate::GlobalState;
use borsh::BorshSerialize;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program::invoke_signed,
    pubkey::Pubkey,
    system_instruction,
    sysvar::{rent::Rent, Sysvar},
};
#[cfg(test)]
use solana_program::msg;

pub fn initialize_global_state(program_id: &Pubkey, accounts: &[AccountInfo]) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("initialize_global_state()");

    let (expected_pda_account, bump_seed) = get_globalstate_pda(program_id);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid GlobalState PubKey"
    );

    if !pda_account.data.borrow().is_empty() {
        let globalstate = globalstate_get(pda_account)?;

        if !globalstate.foundation_allowlist.contains(&doublezero_foundation::id()) {
            process_add_foundation_allowlist_globalconfig(
                program_id,
                accounts,
                &AddFoundationAllowlistGlobalConfigArgs {
                    pubkey: doublezero_foundation::id(),
                },
            )?;
        }

        #[cfg(test)]
        msg!("{:?}", globalstate);

        return Ok(());
    }

    let data = GlobalState {
        account_type: AccountType::GlobalState,
        account_index: 0,
        foundation_allowlist: vec![*payer_account.key],
        device_allowlist: vec![*payer_account.key],
        user_allowlist: vec![*payer_account.key],
    };

    // Size of our index account
    let account_space = data.size();

    // Calculate minimum balance for rent exemption
    let rent = Rent::get()?;
    let required_lamports = rent.minimum_balance(account_space);

    if pda_account.try_borrow_data()?.is_empty() {
        // Create the index account
        invoke_signed(
            &system_instruction::create_account(
                payer_account.key,    // Account paying for the new account
                pda_account.key,      // Account to be created
                required_lamports,    // Amount of lamports to transfer to the new account
                account_space as u64, // Size in bytes to allocate for the data field
                program_id,           // Set program owner to our program
            ),
            &[
                pda_account.clone(),
                payer_account.clone(),
                system_program.clone(),
            ],
            &[&[SEED_PREFIX, SEED_GLOBALSTATE, &[bump_seed]]],
        )?;
    }

    let mut account_data = &mut pda_account.data.borrow_mut()[..];
    data.serialize(&mut account_data).unwrap();

    #[cfg(test)]
    msg!("{:?}", account_data);

    Ok(())
}
