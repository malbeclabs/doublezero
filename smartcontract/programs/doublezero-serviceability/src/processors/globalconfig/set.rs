use crate::{
    error::DoubleZeroError,
    pda::*,
    seeds::{SEED_CONFIG, SEED_PREFIX},
    serializer::try_acc_write,
    state::{
        accounttype::AccountType, exchange::BGP_COMMUNITY_MIN, globalconfig::GlobalConfig,
        globalstate::GlobalState,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::{create_account::try_create_account, types::NetworkV4};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct SetGlobalConfigArgs {
    pub local_asn: u32,
    pub remote_asn: u32,
    pub device_tunnel_block: NetworkV4,
    pub user_tunnel_block: NetworkV4,
    pub multicastgroup_block: NetworkV4,
    pub next_bgp_community: Option<u16>,
}

impl fmt::Debug for SetGlobalConfigArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "local_asn: {}, remote_asn: {}, tunnel_block: {}, user _block: {}, multicastgroup_block: {}, next_bgp_community: {:?}",
            self.local_asn,
            self.remote_asn,
            &self.device_tunnel_block,
            &self.user_tunnel_block,
            &self.multicastgroup_block,
            self.next_bgp_community,
        )
    }
}

pub fn process_set_globalconfig(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetGlobalConfigArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_global_config({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let (expected_pda_account, bump_seed) = get_globalconfig_pda(program_id);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid GlobalConfig PubKey"
    );

    let next_bgp_community = if let Some(val) = value.next_bgp_community {
        val
    } else if pda_account.try_borrow_data()?.is_empty() {
        BGP_COMMUNITY_MIN
    } else {
        GlobalConfig::try_from(pda_account)?.next_bgp_community
    };

    let data: GlobalConfig = GlobalConfig {
        account_type: AccountType::GlobalConfig,
        owner: *payer_account.key,
        bump_seed,
        local_asn: value.local_asn,
        remote_asn: value.remote_asn,
        device_tunnel_block: value.device_tunnel_block,
        user_tunnel_block: value.user_tunnel_block,
        multicastgroup_block: value.multicastgroup_block,
        next_bgp_community,
    };

    let account_space = data.size();

    if pda_account.try_borrow_data()?.is_empty() {
        // Create the index account
        try_create_account(
            payer_account.key,      // Account paying for the new account
            pda_account.key,        // Account to be created
            pda_account.lamports(), // Current amount of lamports on the new account
            account_space,          // Size in bytes to allocate for the data field
            program_id,             // Set program owner to our program
            accounts,
            &[SEED_PREFIX, SEED_CONFIG, &[bump_seed]],
        )?;

        let mut account_data = &mut pda_account.data.borrow_mut()[..];
        data.serialize(&mut account_data).unwrap();
    } else {
        try_acc_write(&data, pda_account, payer_account, accounts)?;
    }

    #[cfg(test)]
    msg!("SetGlobalConfig: {:?}", data);

    Ok(())
}
