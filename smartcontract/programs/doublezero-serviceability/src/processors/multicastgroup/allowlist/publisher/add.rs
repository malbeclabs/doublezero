use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    pda::get_accesspass_pda,
    seeds::{SEED_ACCESS_PASS, SEED_PREFIX},
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::{AccountType, AccountTypeInfo},
        multicastgroup::MulticastGroup,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::{resize_account::resize_account_if_needed, try_create_account};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct AddMulticastGroupPubAllowlistArgs {
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl fmt::Debug for AddMulticastGroupPubAllowlistArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "client_ip: {}, user_payer: {}",
            self.client_ip, self.user_payer
        )
    }
}

pub fn process_add_multicastgroup_pub_allowlist(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &AddMulticastGroupPubAllowlistArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_add_multicastgroup_pub_allowlist({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        mgroup_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(mgroup_account.is_writable, "PDA Account is not writable");

    // Parse the global state account
    let mgroup = MulticastGroup::try_from(mgroup_account)?;
    let globalstate = globalstate_get(globalstate_account)?;

    // Check whether mgroup is authorized
    let is_authorized = (mgroup.owner == *payer_account.key)
        || globalstate.foundation_allowlist.contains(payer_account.key);
    if !is_authorized {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if accesspass_account.data_is_empty() {
        let (expected_pda_account, bump_seed) =
            get_accesspass_pda(program_id, &value.client_ip, &value.user_payer);
        assert_eq!(
            accesspass_account.key, &expected_pda_account,
            "Invalid AccessPass PubKey"
        );
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: value.client_ip,
            user_payer: value.user_payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: *payer_account.key,
            mgroup_pub_allowlist: vec![*mgroup_account.key],
            mgroup_sub_allowlist: vec![],
            flags: 0,
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
                &value.user_payer.to_bytes(),
                &[bump_seed],
            ],
        )?;
        accesspass.try_serialize(accesspass_account)?;
    } else {
        assert_eq!(
            accesspass_account.owner, program_id,
            "Invalid Accesspass Account Owner"
        );

        let mut accesspass = AccessPass::try_from(accesspass_account)?;
        assert!(
            accesspass.client_ip == value.client_ip,
            "AccessPass client_ip does not match"
        );
        assert!(
            accesspass.user_payer == value.user_payer,
            "AccessPass user_payer does not match"
        );

        if !accesspass.mgroup_pub_allowlist.contains(mgroup_account.key) {
            accesspass.mgroup_pub_allowlist.push(*mgroup_account.key);
        }

        resize_account_if_needed(
            accesspass_account,
            payer_account,
            accounts,
            accesspass.size(),
        )?;
        accesspass.try_serialize(accesspass_account)?;
    }

    #[cfg(test)]
    msg!("Updated: {:?}", mgroup);

    Ok(())
}
