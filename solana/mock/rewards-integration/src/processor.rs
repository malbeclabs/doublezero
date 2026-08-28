use borsh::BorshDeserialize;
use doublezero_program_tools::{
    account_info::{try_next_enumerated_account, TryNextAccounts},
    recipe::{
        create_account::try_create_account, create_token_account::try_create_token_account, Invoker,
    },
    zero_copy::{self, ZeroCopyAccount},
};
use doublezero_revenue_distribution::{
    integration::{
        find_integration_bucket_address, find_integration_distribution_address,
        IntegrationInstructionData, WithdrawIntegrationRewardsHandlerAccounts,
        INTEGRATION_DISTRIBUTION_SEED_PREFIX,
    },
    state::TOKEN_2Z_PDA_SEED_PREFIX,
    types::DoubleZeroEpoch,
};
use solana_account_info::AccountInfo;
use solana_cpi::invoke_signed_unchecked;
use solana_msg::msg;
use solana_program_error::{ProgramError, ProgramResult};
use solana_program_pack::Pack;
use solana_pubkey::Pubkey;
use spl_token_interface::instruction as token_instruction;

use crate::{
    instruction::MockRewardsIntegrationInstructionData, state::MockIntegrationDistribution, ID,
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

    match IntegrationInstructionData::try_from_slice(data) {
        Ok(ix) => match ix {
            IntegrationInstructionData::WithdrawIntegrationRewards => {
                try_withdraw_integration_rewards(accounts)
            }
        },
        Err(_) => {
            let ix = BorshDeserialize::try_from_slice(data)
                .map_err(|_| ProgramError::InvalidInstructionData)?;
            match ix {
                MockRewardsIntegrationInstructionData::InitializeIntegrationDistribution {
                    dz_epoch,
                } => try_initialize_integration_distribution(accounts, dz_epoch),
            }
        }
    }
}

fn try_initialize_integration_distribution(
    accounts: &[AccountInfo],
    dz_epoch: DoubleZeroEpoch,
) -> ProgramResult {
    msg!("Initialize integration distribution");

    // We expect the following accounts:
    // - 0: Payer (funder for the new PDAs).
    // - 1: New integration distribution PDA.
    // - 2: New integration 2Z bucket PDA (owned by integration distribution).
    // - 3: 2Z mint.
    // - 4: SPL Token program.
    // - 5: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (_, new_integration_distribution_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_key, bump_seed) = find_integration_distribution_address(&ID, dz_epoch);
    if new_integration_distribution_info.key != &expected_key {
        msg!("Invalid seeds for integration distribution");
        return Err(ProgramError::InvalidSeeds);
    }

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_key,
            signer_seeds: &[
                INTEGRATION_DISTRIBUTION_SEED_PREFIX,
                &dz_epoch.as_seed(),
                &[bump_seed],
            ],
        },
        new_integration_distribution_info.lamports(),
        zero_copy::data_end::<MockIntegrationDistribution>(),
        &ID,
        accounts,
        Default::default(),
    )?;

    let (mut integration_distribution, _) = zero_copy::try_initialize::<MockIntegrationDistribution>(
        new_integration_distribution_info,
    )?;
    integration_distribution.dz_epoch = dz_epoch;
    integration_distribution.bump_seed = bump_seed;
    drop(integration_distribution);

    // Create the 2Z bucket PDA, owned (token authority) by the integration
    // distribution we just created. The derivation matches rev-distr's own
    // 2Z PDA convention so off-chain tools can derive any integration's
    // bucket from its distribution key alone.
    let (_, new_bucket_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;
    let (expected_bucket_key, bucket_bump) = find_integration_bucket_address(&ID, &expected_key);
    if new_bucket_info.key != &expected_bucket_key {
        msg!("Invalid seeds for integration bucket");
        return Err(ProgramError::InvalidSeeds);
    }

    try_create_token_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: new_bucket_info.key,
            signer_seeds: &[
                TOKEN_2Z_PDA_SEED_PREFIX,
                expected_key.as_ref(),
                &[bucket_bump],
            ],
        },
        &doublezero_revenue_distribution::DOUBLEZERO_MINT_KEY,
        &expected_key,
        new_bucket_info.lamports(),
        accounts,
        None,
    )?;

    Ok(())
}

fn try_withdraw_integration_rewards(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Withdraw integration rewards");

    let mut accounts_iter = accounts.iter().enumerate();

    // Read the integration's local epoch from slot 0 first so we can pass it
    // to the shared helper, which validates it against the parent
    // `Distribution`'s `dz_epoch`.
    let (integration_distribution_dz_epoch, integration_distribution_bump_seed) = {
        let info = accounts.first().ok_or(ProgramError::NotEnoughAccountKeys)?;
        let state = ZeroCopyAccount::<MockIntegrationDistribution>::try_from_account_info(
            0,
            info,
            Some(&ID),
        )?;
        (state.dz_epoch, state.bump_seed)
    };

    let interface = WithdrawIntegrationRewardsHandlerAccounts::try_next_accounts(
        &mut accounts_iter,
        integration_distribution_dz_epoch,
    )?;

    let (_, integration_distribution_info) = interface.integration_distribution_info;
    let (_, integration_2z_bucket_info) = interface.integration_2z_bucket_info;
    let (_, destination_token_account_info) = interface.destination_token_account_info;

    let bucket_amount = spl_token_interface::state::Account::unpack(
        &integration_2z_bucket_info.try_borrow_data()?[..],
    )
    .map_err(|_| ProgramError::InvalidAccountData)?
    .amount;

    let token_transfer_ix = token_instruction::transfer(
        &spl_token_interface::ID,
        integration_2z_bucket_info.key,
        destination_token_account_info.key,
        integration_distribution_info.key,
        &[],
        bucket_amount,
    )
    .unwrap();

    invoke_signed_unchecked(
        &token_transfer_ix,
        accounts,
        &[&[
            INTEGRATION_DISTRIBUTION_SEED_PREFIX,
            &integration_distribution_dz_epoch.as_seed(),
            &[integration_distribution_bump_seed],
        ]],
    )?;

    msg!(
        "Integration transferred {} 2Z for DZ epoch {}",
        bucket_amount,
        integration_distribution_dz_epoch,
    );

    Ok(())
}
