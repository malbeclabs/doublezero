use core::fmt;
use std::net::Ipv4Addr;

use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    pda::*,
    seeds::{SEED_ACCESS_PASS, SEED_PREFIX},
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::{AccountType, AccountTypeInfo},
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::{resize_account::resize_account_if_needed, try_create_account};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    clock::Clock,
    entrypoint::ProgramResult,
    msg,
    program::invoke_signed_unchecked,
    pubkey::Pubkey,
    system_instruction,
    sysvar::Sysvar,
};

// Value to rent exempt two `User` accounts + configurable amount for connect/disconnect txns
// `User` account size assumes a single publisher and subscriber pubkey registered
const AIRDROP_USER_RENT_LAMPORTS: u64 = 236 * 2;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct SetAccessPassArgs {
    pub accesspass_type: AccessPassType, // 1 or 33
    pub client_ip: Ipv4Addr,             // 4
    pub last_access_epoch: u64,          // 8
}

impl fmt::Debug for SetAccessPassArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "accesspass_type: {}, ip: {}, last_access_epoch: {}",
            self.accesspass_type, self.client_ip, self.last_access_epoch,
        )
    }
}

pub fn process_set_access_pass(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetAccessPassArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let user_payer = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_accesspass({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        *globalstate_account.owner,
        program_id.clone(),
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(
        accesspass_account.is_writable,
        "PDA Account is not writable"
    );

    let (expected_pda_account, bump_seed) =
        get_accesspass_pda(program_id, &value.client_ip, user_payer.key);
    assert_eq!(
        accesspass_account.key, &expected_pda_account,
        "Invalid AccessPass PubKey"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = globalstate_get(globalstate_account)?;
    if globalstate.sentinel_authority_pk != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        msg!(
            "sentinel_authority_pk: {} payer: {} foundation_allowlist: {:?}",
            globalstate.sentinel_authority_pk,
            payer_account.key,
            globalstate.foundation_allowlist
        );
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let AccessPassType::SolanaValidator(node_id) = value.accesspass_type {
        if node_id == Pubkey::default() {
            msg!("Solana validator access pass type requires a validator pubkey");
            return Err(DoubleZeroError::InvalidSolanaValidatorPubkey.into());
        }
    }

    let clock = Clock::get()?;
    let current_epoch = clock.epoch;

    if value.last_access_epoch > 0 && value.last_access_epoch < current_epoch {
        return Err(DoubleZeroError::InvalidLastAccessEpoch.into());
    }

    // Create the AccessPass account if it doesn't exist, otherwise update it
    if accesspass_account.owner != program_id {
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed,
            accesspass_type: value.accesspass_type,
            client_ip: value.client_ip,
            user_payer: *user_payer.key,
            last_access_epoch: value.last_access_epoch,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: *payer_account.key,
        };

        try_create_account(
            payer_account.key,             // Account paying for the new account
            accesspass_account.key,        // Account to be created
            accesspass_account.lamports(), // Current amount of lamports on the new account
            accesspass.size(),             // Size in bytes to allocate for the data field
            program_id,                    // Set program owner to our program
            accounts,
            &[
                SEED_PREFIX,
                SEED_ACCESS_PASS,
                &value.client_ip.octets(),
                &user_payer.key.to_bytes(),
                &[bump_seed],
            ],
        )?;
        accesspass.try_serialize(accesspass_account)?;

        #[cfg(test)]
        msg!("Created: {:?}", accesspass);
    } else {
        let mut accesspass = if !accesspass_account.data_is_empty() {
            assert_eq!(
                accesspass_account.owner, program_id,
                "Invalid PDA Account Owner"
            );

            AccessPass::try_from(accesspass_account)?
        } else {
            AccessPass {
                account_type: AccountType::AccessPass,
                bump_seed,
                accesspass_type: value.accesspass_type,
                client_ip: value.client_ip,
                user_payer: *user_payer.key,
                last_access_epoch: value.last_access_epoch,
                connection_count: 0,
                status: AccessPassStatus::Requested,
                owner: *payer_account.key,
            }
        };

        accesspass.accesspass_type = value.accesspass_type;
        accesspass.last_access_epoch = value.last_access_epoch;

        resize_account_if_needed(
            accesspass_account,
            payer_account,
            accounts,
            accesspass.size(),
        )?;

        accesspass.try_serialize(accesspass_account)?;

        #[cfg(test)]
        msg!("Updated: {:?}", accesspass);
    }

    let deposit = AIRDROP_USER_RENT_LAMPORTS
        .saturating_add(globalstate.user_airdrop_lamports)
        .saturating_sub(user_payer.lamports());
    if deposit != 0 {
        msg!("Airdropping {} lamports to user", deposit);
        invoke_signed_unchecked(
            &system_instruction::transfer(payer_account.key, user_payer.key, deposit),
            &[
                payer_account.clone(),
                user_payer.clone(),
                system_program.clone(),
            ],
            &[],
        )?;
    }

    Ok(())
}
