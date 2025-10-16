use borsh::BorshDeserialize;
use doublezero_program_tools::{
    account_info::{try_next_enumerated_account, NextAccountOptions, TryNextAccounts},
    instruction::try_build_instruction,
    zero_copy::{self, ZeroCopyMutAccount},
};
use doublezero_revenue_distribution::instruction::{
    account::WithdrawSolAccounts, RevenueDistributionInstructionData,
};
use solana_account_info::AccountInfo;
use solana_cpi::invoke_signed_unchecked;
use solana_msg::msg;
use solana_program_error::{ProgramError, ProgramResult};
use solana_pubkey::Pubkey;
use spl_token::instruction as token_instruction;

use crate::{
    instruction::MockSwapSol2zInstructionData,
    state::{Fill, FillsRegistry, FILLS_CAPACITY},
    ID,
};

solana_program_entrypoint::entrypoint!(try_process_instruction);

fn try_process_instruction(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    data: &[u8],
) -> ProgramResult {
    if program_id != &ID {
        return Err(ProgramError::IncorrectProgramId);
    }

    let ix_data =
        BorshDeserialize::try_from_slice(data).map_err(|_| ProgramError::InvalidInstructionData)?;

    match ix_data {
        MockSwapSol2zInstructionData::InitializeFillsRegistry => {
            try_initialize_fills_registry(accounts)
        }
        MockSwapSol2zInstructionData::BuySol {
            amount_2z_in,
            amount_sol_out,
        } => try_buy_sol(accounts, amount_2z_in, amount_sol_out),
        MockSwapSol2zInstructionData::DequeueFills(max_sol_amount) => {
            try_dequeue_fills(accounts, max_sol_amount)
        }
    }
}

fn try_initialize_fills_registry(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Initialize fills registry");

    let mut accounts_iter = accounts.iter().enumerate();

    let (_, new_fills_registry_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    zero_copy::try_initialize::<FillsRegistry>(new_fills_registry_info)?;

    Ok(())
}

fn try_buy_sol(accounts: &[AccountInfo], amount_2z_in: u64, amount_sol_out: u64) -> ProgramResult {
    msg!("Buy SOL");

    let mut accounts_iter = accounts.iter().enumerate();

    let mut fills_registry =
        ZeroCopyMutAccount::<FillsRegistry>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    if fills_registry.fills_count as usize == FILLS_CAPACITY {
        msg!("Fills registry is full");
        return Err(ProgramError::InvalidAccountData);
    }

    let fills_count = fills_registry.fills_count;
    fills_registry.fills[fills_count as usize] = Fill {
        amount_sol_in: amount_sol_out,
        amount_2z_out: amount_2z_in,
    };
    fills_registry.fills_count = fills_count + 1;

    let (_, src_token_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (_, mint_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (_, dst_token_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (_, transfer_authority_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Transfer 2Z to the swap destination.
    let token_transfer_ix = token_instruction::transfer_checked(
        &spl_token::ID,
        src_token_info.key,
        mint_info.key,
        dst_token_info.key,
        transfer_authority_info.key,
        &[], // signer_pubkeys
        amount_2z_in,
        doublezero_revenue_distribution::DOUBLEZERO_MINT_DECIMALS,
    )
    .unwrap();

    invoke_signed_unchecked(&token_transfer_ix, accounts, &[])?;

    let (_, rd_program_config_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (_, withdraw_authority_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (_, rd_journal_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (_, sol_destination_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let withdraw_sol_ix = try_build_instruction(
        &doublezero_revenue_distribution::ID,
        WithdrawSolAccounts {
            program_config_key: *rd_program_config_info.key,
            withdraw_sol_authority_key: *withdraw_authority_info.key,
            journal_key: *rd_journal_info.key,
            sol_destination_key: *sol_destination_info.key,
        },
        &RevenueDistributionInstructionData::WithdrawSol(amount_sol_out),
    )
    .unwrap();

    let (_, withdraw_authority_bump) =
        doublezero_revenue_distribution::state::find_withdraw_sol_authority_address(&ID);

    invoke_signed_unchecked(
        &withdraw_sol_ix,
        accounts,
        &[&[
            doublezero_revenue_distribution::state::WITHDRAW_SOL_AUTHORITY_SEED_PREFIX,
            &[withdraw_authority_bump],
        ]],
    )?;

    Ok(())
}

fn try_dequeue_fills(accounts: &[AccountInfo], max_sol_amount: u64) -> ProgramResult {
    msg!("Dequeue fills");

    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the configuration registry.
    let (account_index, configuration_registry_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_configuration_registry_key, _) =
        Pubkey::find_program_address(&[b"system_config"], &ID);

    // Enforce this account location.
    if configuration_registry_info.key != &expected_configuration_registry_key {
        msg!(
            "Invalid address for configuration registry (account {})",
            account_index
        );
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 1 must be the program state.
    let (account_index, program_state_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_program_state_key, _) = Pubkey::find_program_address(&[b"state"], &ID);

    // Enforce this account location.
    if program_state_info.key != &expected_program_state_key {
        msg!(
            "Invalid address for program state (account {})",
            account_index
        );
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 2 must be the fills registry.
    let mut fills_registry =
        ZeroCopyMutAccount::<FillsRegistry>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    if fills_registry.fills_count == 0 {
        msg!("Fills registry is empty");
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 3 must be a signer. Enforcing this only to prove out CPI call.
    try_next_enumerated_account(
        &mut accounts_iter,
        NextAccountOptions {
            must_be_signer: true,
            ..Default::default()
        },
    )?;

    let head = fills_registry.head;
    let fill = fills_registry.fills[head as usize];

    if fill.amount_sol_in != max_sol_amount {
        msg!("Fill amount SOL in is not equal to max SOL amount");
        return Err(ProgramError::InvalidAccountData);
    }

    fills_registry.head = (head + 1) % FILLS_CAPACITY as u32;
    fills_registry.fills_count -= 1;

    let mut return_data = [0; 24];
    return_data[..8].copy_from_slice(&max_sol_amount.to_le_bytes());
    return_data[8..16].copy_from_slice(&fill.amount_2z_out.to_le_bytes());
    return_data[16..24].copy_from_slice(&u64::to_le_bytes(1));

    solana_cpi::set_return_data(&return_data);

    Ok(())
}
