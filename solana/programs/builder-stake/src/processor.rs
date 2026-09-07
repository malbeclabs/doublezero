use borsh::BorshDeserialize;
use doublezero_program_tools::{
    account_info::{
        try_next_enumerated_account, EnumeratedAccountInfoIter, TryNextAccounts, UpgradeAuthority,
    },
    recipe::{
        create_account::{try_create_account, CreateAccountOptions},
        create_token_account::try_create_token_account,
        Invoker,
    },
    zero_copy::{self, ZeroCopyAccount, ZeroCopyMutAccount},
};
use solana_account_info::{AccountInfo, MAX_PERMITTED_DATA_INCREASE};
use solana_cpi::invoke_signed_unchecked;
use solana_msg::msg;
use solana_program_error::{ProgramError, ProgramResult};
use solana_program_pack::Pack;
use solana_pubkey::Pubkey;
use solana_sysvar::{rent::Rent, Sysvar};
use spl_token_interface::instruction as token_instruction;

use crate::{
    instruction::{BuilderStakeInstructionData, ProgramConfiguration, ProgramFlagConfiguration},
    state::{self, BuilderStake, ProgramConfig},
    DOUBLEZERO_MINT_KEY, ID,
};

// A change to either size means every deployed account of that type has to be migrated, so make
// the change deliberate rather than incidental.
const _: () = assert!(size_of::<BuilderStake>() == 136);
const _: () = assert!(size_of::<ProgramConfig>() == 176);

solana_program_entrypoint::entrypoint!(try_process_instruction);

fn try_process_instruction(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    data: &[u8],
) -> ProgramResult {
    if program_id != &ID {
        return Err(ProgramError::IncorrectProgramId);
    }

    // Instruction data that deserializes to a variant but carries trailing bytes is invalid, not
    // a variant with junk after it.
    let ix_data =
        BorshDeserialize::try_from_slice(data).map_err(|_| ProgramError::InvalidInstructionData)?;

    match ix_data {
        BuilderStakeInstructionData::InitializeProgram => try_initialize_program(accounts),
        BuilderStakeInstructionData::SetAdmin(admin_key) => try_set_admin(accounts, admin_key),
        BuilderStakeInstructionData::ConfigureProgram(setting) => {
            try_configure_program(accounts, setting)
        }
        BuilderStakeInstructionData::InitializeBuilderStake {
            stake_index,
            committed_rate_bits_per_sec,
        } => try_initialize_builder_stake(accounts, stake_index, committed_rate_bits_per_sec),
        BuilderStakeInstructionData::Deposit { amount } => try_deposit(accounts, amount),
    }
}

fn try_initialize_program(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Initialize program");

    // We expect the following accounts for this instruction:
    // - 0: Payer.
    // - 1: New program config.
    // - 2: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be a signer and writable because it funds the new program config. The
    // create-account workflow requires both, so we do not check them here.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 1 must be the new program config.
    let (account_index, new_program_config_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_program_config_key, program_config_bump) = ProgramConfig::find_address();

    if new_program_config_info.key != &expected_program_config_key {
        msg!(
            "Invalid seeds for program config (account {})",
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    // Allocated at the maximum so later settings, like the tier table, need no realloc.
    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_program_config_key,
            signer_seeds: &[ProgramConfig::SEED_PREFIX, &[program_config_bump]],
        },
        new_program_config_info.lamports(),
        MAX_PERMITTED_DATA_INCREASE,
        &ID,
        accounts,
        CreateAccountOptions {
            rent_sysvar: Some(&Rent::get().unwrap()),
            additional_lamports: None,
        },
    )?;

    let (mut program_config, _) =
        zero_copy::try_initialize::<ProgramConfig>(new_program_config_info)?;
    program_config.bump_seed = program_config_bump;

    // A fresh deployment has no admin and no tier table, so it must not take deposits yet.
    msg!("Pause program");
    program_config.set_is_paused(true);

    Ok(())
}

fn try_set_admin(accounts: &[AccountInfo], admin_key: Pubkey) -> ProgramResult {
    msg!("Set admin");

    // We expect the following accounts for this instruction:
    // - 0: Program data.
    // - 1: Upgrade authority.
    // - 2: Program config.
    let mut accounts_iter = accounts.iter().enumerate();

    // Accounts 0 and 1 prove the signer is this program's upgrade authority.
    UpgradeAuthority::try_next_accounts(&mut accounts_iter, &ID)?;

    let mut program_config =
        ZeroCopyMutAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    msg!("admin_key: {}", admin_key);
    program_config.admin_key = admin_key;

    Ok(())
}

fn try_configure_program(accounts: &[AccountInfo], setting: ProgramConfiguration) -> ProgramResult {
    msg!("Configure program");

    // We expect the following accounts for this instruction:
    // - 0: Admin.
    // - 1: Program config.
    let mut accounts_iter = accounts.iter().enumerate();

    let (account_index, admin_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    if !admin_info.is_signer {
        msg!("Admin must be a signer (account {})", account_index);
        return Err(ProgramError::MissingRequiredSignature);
    }

    let mut program_config =
        ZeroCopyMutAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;
    program_config.try_require_admin(admin_info.key)?;

    match setting {
        ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(paused)) => {
            msg!("is_paused: {}", paused);
            program_config.set_is_paused(paused);
        }
    }

    Ok(())
}

fn try_initialize_builder_stake(
    accounts: &[AccountInfo],
    stake_index: u64,
    committed_rate_bits_per_sec: u64,
) -> ProgramResult {
    msg!("Initialize builder stake");

    // A stake that backs no rate backs nothing. Reject it here rather than letting a builder lock
    // 2Z against a commitment no feed can use.
    if committed_rate_bits_per_sec == 0 {
        msg!("Committed rate must be greater than zero");
        return Err(ProgramError::InvalidInstructionData);
    }

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Builder (payer).
    // - 2: New builder stake.
    // - 3: New builder stake 2Z token account.
    // - 4: SPL 2Z mint.
    // - 5: SPL Token program.
    // - 6: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;
    program_config.try_require_unpaused()?;

    // Account 1 funds both new accounts and is the builder the stake belongs to. The
    // create-account workflow requires it to be a writable signer.
    let (account_index, builder_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    if !builder_info.is_signer {
        msg!("Builder must be a signer (account {})", account_index);
        return Err(ProgramError::MissingRequiredSignature);
    }

    let (account_index, new_builder_stake_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_builder_stake_key, builder_stake_bump) =
        BuilderStake::find_address(builder_info.key, stake_index);

    if new_builder_stake_info.key != &expected_builder_stake_key {
        msg!(
            "Invalid seeds for builder stake (account {})",
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    let rent_sysvar = Rent::get().unwrap();

    try_create_account(
        Invoker::Signer(builder_info.key),
        Invoker::Pda {
            key: &expected_builder_stake_key,
            signer_seeds: &[
                BuilderStake::SEED_PREFIX,
                builder_info.key.as_ref(),
                &stake_index.to_le_bytes(),
                &[builder_stake_bump],
            ],
        },
        new_builder_stake_info.lamports(),
        zero_copy::data_end::<BuilderStake>(),
        &ID,
        accounts,
        CreateAccountOptions {
            rent_sysvar: Some(&rent_sysvar),
            additional_lamports: None,
        },
    )?;

    // Account 3 must be this stake's 2Z token account, owned by the stake PDA.
    let (_, new_token_account_info, token_account_bump) =
        try_next_2z_token_pda_info(&mut accounts_iter, &expected_builder_stake_key, None)?;

    // Account 4 must be the 2Z mint, needed to initialize the token account.
    try_next_2z_mint_info(&mut accounts_iter)?;

    // Account 5 must be the SPL Token program, which initializes the token account.
    try_next_token_program_info(&mut accounts_iter)?;

    try_create_token_account(
        Invoker::Signer(builder_info.key),
        Invoker::Pda {
            key: new_token_account_info.key,
            signer_seeds: &[
                state::TOKEN_2Z_PDA_SEED_PREFIX,
                expected_builder_stake_key.as_ref(),
                &[token_account_bump],
            ],
        },
        &DOUBLEZERO_MINT_KEY,
        &expected_builder_stake_key,
        new_token_account_info.lamports(),
        accounts,
        Some(&rent_sysvar),
    )?;

    let (mut builder_stake, _) = zero_copy::try_initialize::<BuilderStake>(new_builder_stake_info)?;
    builder_stake.builder = *builder_info.key;
    builder_stake.stake_index = stake_index;
    builder_stake.committed_rate_bits_per_sec = committed_rate_bits_per_sec;
    builder_stake.bump_seed = builder_stake_bump;
    builder_stake.token_account_bump_seed = token_account_bump;

    msg!(
        "Builder {} stake {} committed to {} bits/sec",
        builder_info.key,
        stake_index,
        committed_rate_bits_per_sec
    );

    Ok(())
}

fn try_deposit(accounts: &[AccountInfo], amount: u64) -> ProgramResult {
    msg!("Deposit");

    if amount == 0 {
        msg!("Deposit amount must be greater than zero");
        return Err(ProgramError::InvalidInstructionData);
    }

    // We expect the following accounts for this instruction:
    // - 0: Program config.
    // - 1: Builder.
    // - 2: Builder stake.
    // - 3: Builder stake 2Z token account.
    // - 4: Source 2Z token account.
    // - 5: SPL Token program.
    let mut accounts_iter = accounts.iter().enumerate();

    let program_config =
        ZeroCopyAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;
    program_config.try_require_unpaused()?;

    // Account 1 signs the token transfer out of the source account, so the source account's
    // authority is what actually gates this instruction.
    let (account_index, builder_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    if !builder_info.is_signer {
        msg!("Builder must be a signer (account {})", account_index);
        return Err(ProgramError::MissingRequiredSignature);
    }

    let mut builder_stake =
        ZeroCopyMutAccount::<BuilderStake>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    // The stake's own address encodes its builder, so a mismatch here means the caller passed
    // somebody else's stake.
    if &builder_stake.builder != builder_info.key {
        msg!("Stake belongs to builder {}", builder_stake.builder);
        return Err(ProgramError::IncorrectAuthority);
    }

    // Account 3 must be this stake's 2Z token account. Checked against the cached bump so a
    // caller cannot redirect the deposit to another account.
    let (_, stake_token_account_info, _) = try_next_2z_token_pda_info(
        &mut accounts_iter,
        builder_stake.info.key,
        Some(builder_stake.token_account_bump_seed),
    )?;

    // Account 4 must be the source of the tokens. The token program checks its mint and owner
    // when it processes the transfer, so we do not repeat that here.
    let (_, source_token_account_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 5 must be the SPL Token program.
    try_next_token_program_info(&mut accounts_iter)?;

    let token_transfer_ix = token_instruction::transfer(
        &spl_token_interface::ID,
        source_token_account_info.key,
        stake_token_account_info.key,
        builder_info.key,
        &[], // signer_pubkeys
        amount,
    )
    .unwrap();

    invoke_signed_unchecked(&token_transfer_ix, accounts, &[])?;

    // Read the balance back rather than adding to the stored figure. The token account is the
    // authority on what is held, and a transfer sent to it outside this instruction would
    // otherwise leave the two disagreeing forever.
    builder_stake.deposited_2z_amount = try_token_account_amount(stake_token_account_info)?;

    msg!(
        "Deposited {} 2Z, stake now holds {}",
        amount,
        builder_stake.deposited_2z_amount
    );

    Ok(())
}

//
// Helpers.
//

#[inline(always)]
fn try_next_2z_token_pda_info<'a, 'b>(
    accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
    token_owner: &Pubkey,
    token_pda_bump: Option<u8>,
) -> Result<(usize, &'a AccountInfo<'b>, u8), ProgramError> {
    let (account_index, token_pda_info) =
        try_next_enumerated_account(accounts_iter, Default::default())?;

    let (expected_token_pda_key, token_pda_bump) = match token_pda_bump {
        Some(bump_seed) => {
            let expected_pda_key = state::checked_2z_token_pda_address(token_owner, bump_seed)
                .ok_or_else(|| {
                    msg!(
                        "Failed to create 2Z token PDA address with bump seed (account {})",
                        account_index
                    );
                    ProgramError::InvalidSeeds
                })?;

            (expected_pda_key, bump_seed)
        }
        None => state::find_2z_token_pda_address(token_owner),
    };

    if token_pda_info.key != &expected_token_pda_key {
        msg!("Invalid seeds for 2Z token PDA (account {})", account_index);
        return Err(ProgramError::InvalidSeeds);
    }

    Ok((account_index, token_pda_info, token_pda_bump))
}

#[inline(always)]
fn try_next_2z_mint_info(accounts_iter: &mut EnumeratedAccountInfoIter) -> ProgramResult {
    let (account_index, mint_2z_info) =
        try_next_enumerated_account(accounts_iter, Default::default())?;

    if mint_2z_info.key != &DOUBLEZERO_MINT_KEY {
        msg!("Invalid address for 2Z mint (account {})", account_index);
        return Err(ProgramError::InvalidAccountData);
    }

    Ok(())
}

#[inline(always)]
fn try_next_token_program_info(accounts_iter: &mut EnumeratedAccountInfoIter) -> ProgramResult {
    let (account_index, token_program_info) =
        try_next_enumerated_account(accounts_iter, Default::default())?;

    if token_program_info.key != &spl_token_interface::ID {
        msg!(
            "Invalid address for SPL Token program (account {})",
            account_index
        );
        return Err(ProgramError::IncorrectProgramId);
    }

    Ok(())
}

#[inline(always)]
fn try_token_account_amount(info: &AccountInfo) -> Result<u64, ProgramError> {
    Ok(spl_token_interface::state::Account::unpack(&info.data.borrow()[..])?.amount)
}
