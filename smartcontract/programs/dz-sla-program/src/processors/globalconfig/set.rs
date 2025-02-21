use crate::pda::*;
use crate::{
    seeds::*,
    state::{accounttype::AccountType, globalconfig::*},
    types::NetworkV4,
};
use borsh::{BorshDeserialize, BorshSerialize};
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

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct SetGlobalConfigArgs {
    pub local_asn: u32,
    pub remote_asn: u32,
    pub tunnel_tunnel_block: NetworkV4,
    pub user_tunnel_block: NetworkV4,
}

pub fn process_set_globalconfig(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetGlobalConfigArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_global_config({:?})", value);

    let (expected_pda_account, expected_bump_seed) = get_globalconfig_pda(program_id);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid GlobalConfig PubKey"
    );

    let data: GlobalConfig = GlobalConfig {
        account_type: AccountType::Config,
        owner: *payer_account.key,
        local_asn: value.local_asn,
        remote_asn: value.remote_asn,
        tunnel_tunnel_block: value.tunnel_tunnel_block,
        user_tunnel_block: value.user_tunnel_block,
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
            &[&[SEED_PREFIX, SEED_CONFIG, &[expected_bump_seed]]],
        )?;
    }

    let mut account_data = &mut pda_account.data.borrow_mut()[..];
    data.serialize(&mut account_data).unwrap();

    #[cfg(test)]
    msg!("SetGlobalConfig: {:?}", data);

    Ok(())
}
