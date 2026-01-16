use crate::{
    error::DoubleZeroError,
    pda::*,
    seeds::{SEED_ACCESS_PASS, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType, ALLOW_MULTIPLE_IP, IS_DYNAMIC},
        accounttype::AccountType,
        globalstate::GlobalState,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    clock::Clock,
    entrypoint::ProgramResult,
    msg,
    program::invoke_signed_unchecked,
    pubkey::Pubkey,
    rent::Rent,
    system_instruction,
    sysvar::Sysvar,
};
use std::net::Ipv4Addr;

// Value to rent exempt two `User` accounts + configurable amount for connect/disconnect txns
// `User` account size assumes a single publisher and subscriber pubkey registered
const AIRDROP_USER_RENT_LAMPORTS_BYTES: usize = 236 * 3; // 236 bytes per User account x 3 accounts = 708 bytes

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct SetAccessPassArgs {
    pub accesspass_type: AccessPassType, // 1 or 33
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr, // 4
    pub last_access_epoch: u64,          // 8
    pub allow_multiple_ip: bool,         // 1
}

impl fmt::Debug for SetAccessPassArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "accesspass_type: {}, ip: {}, last_access_epoch: {}, allow_multiple_ip: {}",
            self.accesspass_type, self.client_ip, self.last_access_epoch, self.allow_multiple_ip,
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

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

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
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
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
            return Err(DoubleZeroError::InvalidSolanaPubkey.into());
        }
    }

    let clock = Clock::get()?;
    let current_epoch = clock.epoch;

    if value.last_access_epoch > 0 && value.last_access_epoch < current_epoch {
        return Err(DoubleZeroError::InvalidLastAccessEpoch.into());
    }

    // Flags
    let mut flags = 0;
    if value.client_ip == Ipv4Addr::UNSPECIFIED {
        flags |= IS_DYNAMIC;
    }
    if value.allow_multiple_ip {
        flags |= ALLOW_MULTIPLE_IP;
    }

    // If account does not exist, create it
    if *accesspass_account.owner == solana_program::system_program::id() {
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed,
            accesspass_type: value.accesspass_type.clone(),
            client_ip: value.client_ip,
            user_payer: *user_payer.key,
            last_access_epoch: value.last_access_epoch,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: *payer_account.key,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags,
        };

        try_acc_create(
            &accesspass,
            accesspass_account,
            payer_account,
            system_program,
            program_id,
            &[
                SEED_PREFIX,
                SEED_ACCESS_PASS,
                &value.client_ip.octets(),
                &user_payer.key.to_bytes(),
                &[bump_seed],
            ],
        )?;

        #[cfg(test)]
        msg!("Created: {:?}", accesspass);
    } else {
        // Read or create Access Pass
        // Old bug where close accounts were not fully zeroed out instead of being closed
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
                accesspass_type: value.accesspass_type.clone(),
                client_ip: value.client_ip,
                flags,
                user_payer: *user_payer.key,
                last_access_epoch: value.last_access_epoch,
                connection_count: 0,
                status: AccessPassStatus::Requested,
                owner: *payer_account.key,
                mgroup_pub_allowlist: vec![],
                mgroup_sub_allowlist: vec![],
            }
        };

        // Update fields
        accesspass.accesspass_type = value.accesspass_type.clone();
        accesspass.last_access_epoch = value.last_access_epoch;
        accesspass.flags = flags;

        // Write back updated Access Pass
        try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

        #[cfg(test)]
        msg!("Updated: {:?}", accesspass);
    }

    // Airdrop rent exempt + configured lamports to user_payer account
    let deposit = Rent::get()
        .unwrap()
        .minimum_balance(AIRDROP_USER_RENT_LAMPORTS_BYTES)
        .saturating_add(globalstate.user_airdrop_lamports)
        .saturating_sub(user_payer.lamports());

    msg!("Airdropping {} lamports to user account", deposit);
    invoke_signed_unchecked(
        &system_instruction::transfer(payer_account.key, user_payer.key, deposit),
        &[
            payer_account.clone(),
            user_payer.clone(),
            system_program.clone(),
        ],
        &[],
    )?;

    Ok(())
}
