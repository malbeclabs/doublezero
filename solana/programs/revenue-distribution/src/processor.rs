use borsh::BorshDeserialize;
use doublezero_program_tools::{
    account_info::{
        try_next_enumerated_account, EnumeratedAccountInfoIter, NextAccountOptions,
        TryNextAccounts, UpgradeAuthority,
    },
    recipe::{create_account::try_create_account, Invoker},
    zero_copy::{self, ZeroCopyAccount, ZeroCopyMutAccount},
};
use solana_account_info::AccountInfo;
use solana_cpi::invoke_signed_unchecked;
use solana_msg::msg;
use solana_program_error::{ProgramError, ProgramResult};
use solana_program_pack::Pack;
use solana_pubkey::Pubkey;
use solana_sysvar::{rent::Rent, Sysvar};

use crate::{
    instruction::{
        ConfigureDistributionData, ConfigureFlag, ConfigureProgramSetting,
        RevenueDistributionInstructionData,
    },
    state::{
        self, CommunityBurnRateParameters, Distribution, Journal, ProgramConfig,
        CUSTODIED_2Z_SEED_PREFIX,
    },
    types::{BurnRate, ValidatorFee},
    DOUBLEZERO_MINT, ID,
};

solana_program_entrypoint::entrypoint!(process_instruction);

fn process_instruction(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    data: &[u8],
) -> ProgramResult {
    if program_id != &ID {
        return Err(ProgramError::IncorrectProgramId);
    }

    // NOTE: Instruction data that happens to deserialize to any of the enum variants and has
    // trailing data constitutes invalid instruction data.
    match BorshDeserialize::try_from_slice(data) {
        Err(_) => Err(ProgramError::InvalidInstructionData),
        Ok(RevenueDistributionInstructionData::InitializeProgram) => {
            process_initialize_program(accounts)
        }
        Ok(RevenueDistributionInstructionData::SetAdmin(admin_key)) => {
            process_set_admin(accounts, admin_key)
        }
        Ok(RevenueDistributionInstructionData::ConfigureProgram(setting)) => {
            process_configure_program(accounts, setting)
        }
        Ok(RevenueDistributionInstructionData::InitializeJournal) => {
            process_initialize_journal(accounts)
        }
        Ok(RevenueDistributionInstructionData::InitializeDistribution) => {
            process_initialize_distribution(accounts)
        }
        Ok(RevenueDistributionInstructionData::ConfigureDistribution(data)) => {
            process_configure_distribution(accounts, data)
        }
    }
}

fn process_initialize_program(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Initialize program");

    // We expect 4 accounts for this instruction at the following indices:
    // - 0: Payer (funder for new accounts).
    // - 1: New program config account.
    // - 2: SOL custody account.
    // - 3: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be a signer and writable (i.e., payer) because it will be sending lamports
    // to the new config account when the system program allocates data to it. But because the
    // create-program instruction requires that this account is a signer and is writable, we do
    // not need to explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 1 must be the new program config account. This account should not exist yet.
    let (account_index, new_program_config_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_program_config_key, program_config_bump) = ProgramConfig::find_address();

    // Enforce this account location.
    if new_program_config_info.key != &expected_program_config_key {
        msg!(
            "Invalid seeds for program config (account {})",
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_program_config_key,
            signer_seeds: &[ProgramConfig::SEED_PREFIX, &[program_config_bump]],
        },
        new_program_config_info.lamports(),
        zero_copy::data_end::<ProgramConfig>(),
        &ID,
        accounts,
        None, // rent_sysvar
    )?;

    // This simply adds the discriminator.
    let (mut program_config, _) =
        zero_copy::try_initialize::<ProgramConfig>(new_program_config_info, None)?;

    // Initially, the program will be paused. Other fields will be set with separate instructions.
    msg!("Pause program");
    program_config.set_is_paused(true);

    Ok(())
}

fn process_set_admin(accounts: &[AccountInfo], admin_key: Pubkey) -> ProgramResult {
    msg!("Set admin");

    // We expect 3 accounts for this instruction at the following indices:
    // - 0: This program's program data account (BPF Loader Upgradeable program).
    // - 1: The program's owner (i.e., upgrade authority).
    // - 2: Program config.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be the program data belonging to this program.
    // Account 1 must be the owner of the program data (i.e., the upgrade authority).
    UpgradeAuthority::try_next_accounts(&mut accounts_iter, &ID)?;

    // Account 2 must be the program config account.
    let mut program_config =
        ZeroCopyMutAccount::<ProgramConfig>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    msg!("admin_key: {}", admin_key);
    program_config.admin_key = admin_key;

    Ok(())
}

fn process_configure_program(
    accounts: &[AccountInfo],
    setting: ConfigureProgramSetting,
) -> ProgramResult {
    msg!("Configure program");

    // We expect 2 accounts for this instruction at the following indices:
    // - 0: Program config.
    // - 1: Admin.
    let mut accounts_iter = accounts.iter().enumerate();

    let authorized_use =
        VerifiedProgramAuthorityMut::try_next_accounts(&mut accounts_iter, Authority::Admin)?;
    let mut program_config = authorized_use.program_config;

    match setting {
        ConfigureProgramSetting::Flag(configure_flag) => {
            msg!("Set flag");
            match configure_flag {
                ConfigureFlag::IsPaused(should_pause) => {
                    msg!("is_paused: {}", should_pause);
                    program_config.set_is_paused(should_pause);
                }
            };
        }
        ConfigureProgramSetting::Accountant(accountant_key) => {
            msg!("Set accountant_key: {}", accountant_key);
            program_config.accountant_key = accountant_key;
        }
        ConfigureProgramSetting::Sol2zSwapProgram(sol_2z_swap_program_id) => {
            msg!("Set sol_2z_swap_program_id: {}", sol_2z_swap_program_id);
            program_config.sol_2z_swap_program_id = sol_2z_swap_program_id;
        }
        ConfigureProgramSetting::SolanaValidatorFee(solana_validator_fee) => {
            let solana_validator_fee =
                ValidatorFee::new(solana_validator_fee).ok_or_else(|| {
                    msg!(
                        "Invalid Solana validator fee: {}/{}",
                        solana_validator_fee,
                        10_000
                    );
                    ProgramError::InvalidInstructionData
                })?;

            msg!("Set solana_validator_fee: {}", solana_validator_fee);
            program_config.current_solana_validator_fee = solana_validator_fee;
        }
        ConfigureProgramSetting::CalculationGracePeriodSeconds(
            calculation_grace_period_seconds,
        ) => {
            msg!(
                "Set calculation_grace_period_seconds: {}",
                calculation_grace_period_seconds
            );
            program_config.calculation_grace_period_seconds = calculation_grace_period_seconds;
        }
        ConfigureProgramSetting::CommunityBurnRateParameters {
            limit,
            dz_epochs_to_increasing,
            dz_epochs_to_limit,
            initial_rate,
        } => {
            let limit = BurnRate::new(limit).ok_or_else(|| {
                msg!("Invalid community burn rate limit: {}", limit);
                ProgramError::InvalidInstructionData
            })?;

            match initial_rate {
                // We only allow specifying the initial rate if the accountant has not initialized
                // any distributions yet.
                Some(initial_rate) => {
                    // When the accountant initializes a new distribution, the initialize-distribution
                    // instruction first checks whether the last community burn rate is non-zero. If there
                    // is a non-zero value, a new community burn rate will be calculated for this DZ epoch.
                    // This updated community burn rate will be saved to the program config. Finally, the
                    // program config's next DZ epoch will increase by one.
                    if program_config.next_dz_epoch != 0 {
                        msg!(
                            "Cannot initialize community burn rate parameters if not zero DZ epoch"
                        );
                        return Err(ProgramError::InvalidInstructionData);
                    }

                    let initial_rate = BurnRate::new(initial_rate).ok_or_else(|| {
                        msg!("Invalid initial community burn rate: {}", initial_rate);
                        ProgramError::InvalidInstructionData
                    })?;

                    let cbr_params = CommunityBurnRateParameters::new(
                        initial_rate,
                        limit,
                        dz_epochs_to_increasing,
                        dz_epochs_to_limit,
                    )
                    .ok_or_else(|| {
                        msg!("Invalid initial community burn rate parameters");
                        msg!("  initial_rate: {}", initial_rate);
                        msg!("  limit: {}", limit);
                        msg!("  dz_epochs_to_increasing: {}", dz_epochs_to_increasing);
                        msg!("  dz_epochs_to_limit: {}", dz_epochs_to_limit);
                        ProgramError::InvalidInstructionData
                    })?;

                    msg!("Set initial community_burn_rate_parameters");
                    msg!("  initial_rate: {}", initial_rate);
                    msg!("  limit: {}", limit);
                    msg!("  dz_epochs_to_increasing: {}", dz_epochs_to_increasing);
                    msg!("  dz_epochs_to_limit: {}", dz_epochs_to_limit);

                    let (slope_numerator, slope_denominator) = cbr_params.slope();
                    msg!("  slope_numerator: {}", slope_numerator);
                    msg!("  slope_denominator: {}", slope_denominator);

                    program_config.community_burn_rate_parameters = cbr_params;
                }
                None => {
                    let cbr_params = &mut program_config.community_burn_rate_parameters;

                    let (new_slope_numerator, new_slope_denominator) = cbr_params
                        .checked_update(limit, dz_epochs_to_increasing, dz_epochs_to_limit)
                        .ok_or_else(|| {
                            msg!("Cannot update community burn rate parameters");
                            msg!(
                                "  cached_last_burn_rate: {}",
                                cbr_params.next_burn_rate().unwrap()
                            );
                            msg!("  new_limit: {}", limit);
                            msg!("  new_dz_epochs_to_increasing: {}", dz_epochs_to_increasing);
                            msg!("  new_dz_epochs_to_limit: {}", dz_epochs_to_limit);
                            ProgramError::InvalidInstructionData
                        })?;

                    msg!("Update community_burn_rate_parameters");
                    msg!("  limit: {}", limit);
                    msg!("  dz_epochs_to_increasing: {}", dz_epochs_to_increasing);
                    msg!("  dz_epochs_to_limit: {}", dz_epochs_to_limit);
                    msg!("  slope_numerator: {}", new_slope_numerator);
                    msg!("  slope_denominator: {}", new_slope_denominator);
                }
            }
        }
    }

    Ok(())
}

fn process_initialize_journal(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Initialize journal");

    // We expect 6 accounts for this instruction at the following indices:
    // - 0: Payer (funder for new accounts).
    // - 1: New journal account.
    // - 2: New journal's 2Z custody token account.
    // - 3: 2Z mint account.
    // - 4: SPL Token program.
    // - 5: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    // Account 0 must be a signer and writable (i.e., payer) because it will be sending lamports
    // to the new journal account when the system program allocates data to it. But because the
    // create-program instruction requires that this account is a signer and is writable, we do
    // not need to explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 1 must be the new journal account. This account should not exist yet.
    let (account_index, new_journal_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_journal_key, journal_bump) = Journal::find_address();

    // Enforce this account location.
    if new_journal_info.key != &expected_journal_key {
        msg!("Invalid seeds for journal (account {})", account_index);
        return Err(ProgramError::InvalidSeeds);
    }

    // We declare this because Rent will be used multiple times in this instruction.
    let rent_sysvar = Rent::get().unwrap();

    const JOURNAL_LEN: usize = {
        zero_copy::data_end::<Journal>() // header length
        + 4 // Reserved 4 bytes for encoding Vec::len()
    };

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_journal_key,
            signer_seeds: &[Journal::SEED_PREFIX, &[journal_bump]],
        },
        new_journal_info.lamports(),
        JOURNAL_LEN,
        &ID,
        accounts,
        Some(&rent_sysvar),
    )?;

    // This simply adds the discriminator. Other fields will be set with separate instructions.
    zero_copy::try_initialize::<Journal>(new_journal_info, None)?;

    // Account 2 must be the new 2Z custody token account. This account should not exist yet.
    let (account_index, new_2z_custody_token_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_custodied_2z_key, custodied_2z_bump) =
        state::find_custodied_2z_address(&expected_journal_key);

    // Enforce this account location.
    if new_2z_custody_token_info.key != &expected_custodied_2z_key {
        msg!(
            "Invalid seeds for journal's 2Z custody token (account {})",
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_custodied_2z_key,
            signer_seeds: &[
                CUSTODIED_2Z_SEED_PREFIX,
                expected_journal_key.as_ref(),
                &[custodied_2z_bump],
            ],
        },
        new_2z_custody_token_info.lamports(),
        spl_token::state::Account::LEN,
        &spl_token::ID,
        accounts,
        Some(&rent_sysvar),
    )?;

    // Account 3 must be the 2Z mint.
    let (account_index, spl_2z_mint_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Enforce this account location.
    if spl_2z_mint_info.key != &DOUBLEZERO_MINT {
        msg!("Invalid address for 2Z mint (account {})", account_index);
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 4 must be the SPL Token program.
    let (account_index, spl_token_program_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Enforce this account location.
    if spl_token_program_info.key != &spl_token::ID {
        msg!(
            "Invalid address for SPL Token program (account {})",
            account_index
        );
        return Err(ProgramError::InvalidAccountData);
    }

    let initialize_token_account_ix = spl_token::instruction::initialize_account3(
        &spl_token::ID,
        &expected_custodied_2z_key,
        &DOUBLEZERO_MINT,
        &expected_journal_key,
    )
    .unwrap();

    invoke_signed_unchecked(&initialize_token_account_ix, accounts, &[])?;

    Ok(())
}

fn process_initialize_distribution(accounts: &[AccountInfo]) -> ProgramResult {
    msg!("Initialize distribution");

    // We expect 8 accounts for this instruction at the following indices:
    // - 0: Program config account.
    // - 1: Accountant.
    // - 2: Payer (funder for new accounts).
    // - 3: New distribution account.
    // - 4: New distribution's 2Z custody token account.
    // - 5: 2Z mint account.
    // - 6: SPL Token program.
    // - 7: System program.
    let mut accounts_iter = accounts.iter().enumerate();

    let authorized_use =
        VerifiedProgramAuthorityMut::try_next_accounts(&mut accounts_iter, Authority::Accountant)?;
    let mut program_config = authorized_use.program_config;

    // Cannot initialize a new distribution when paused.
    // Before we initialize a new distribution, we need to make sure of the following:
    // 1. The program is not paused.
    // 2. Solana validator fee is not zero.
    // 3. The last community burn rate is not zero.

    if program_config.is_paused() {
        msg!("Program paused");
        return Err(ProgramError::InvalidAccountData);
    }

    if program_config.current_solana_validator_fee == ValidatorFee::MIN {
        msg!("Solana validator fee has not been configured yet");
        return Err(ProgramError::InvalidAccountData);
    }

    if program_config
        .community_burn_rate_parameters
        .next_burn_rate()
        .is_none()
    {
        msg!("Community burn rate has not been configured yet");
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 2 must be a signer and writable (i.e., payer) because it will be sending lamports
    // to the new journal account when the system program allocates data to it. But because the
    // create-program instruction requires that this account is a signer and is writable, we do
    // not need to explicitly check these fields in its account info.
    let (_, payer_info) = try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Account 3 must be the new distribution account. This account should not exist yet.
    let (account_index, new_distribution_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // We will need this DZ epoch for the distribution account.
    let dz_epoch = program_config.next_dz_epoch;

    let community_burn_rate = program_config
        .community_burn_rate_parameters
        .checked_compute()
        .ok_or_else(|| {
            msg!("Community burn rate parameters are misconfigured");
            ProgramError::InvalidAccountData
        })?;

    // Uptick the program config's next epoch.
    program_config.next_dz_epoch = dz_epoch + 1;

    // We no longer need the program config for anything.
    drop(program_config);

    let (expected_distribution_key, distribution_bump) = Distribution::find_address(dz_epoch);

    // Enforce this account location.
    if new_distribution_info.key != &expected_distribution_key {
        msg!("Invalid seeds for distribution (account {})", account_index);
        return Err(ProgramError::InvalidSeeds);
    }

    // We declare this because Rent will be used multiple times in this instruction.
    let rent_sysvar = Rent::get().unwrap();

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_distribution_key,
            signer_seeds: &[
                Distribution::SEED_PREFIX,
                &dz_epoch.as_seed(),
                &[distribution_bump],
            ],
        },
        new_distribution_info.lamports(),
        zero_copy::data_end::<Distribution>(),
        &ID,
        accounts,
        Some(&rent_sysvar),
    )?;

    // This simply adds the discriminator. Other fields will be set with separate instructions.
    let (mut distribution, _) =
        zero_copy::try_initialize::<Distribution>(new_distribution_info, None)?;

    // Set DZ epoch. The DZ epoch should never change with any interaction with the epoch
    // distribution account.
    distribution.dz_epoch = dz_epoch;
    distribution.community_burn_rate = community_burn_rate;

    // Account 2 must be the new 2Z custody token account. This account should not exist yet.
    let (account_index, new_2z_custody_token_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    let (expected_custodied_2z_key, custodied_2z_bump) =
        state::find_custodied_2z_address(&expected_distribution_key);

    // Enforce this account location.
    if new_2z_custody_token_info.key != &expected_custodied_2z_key {
        msg!(
            "Invalid seeds for distribution's 2Z custody token (account {})",
            account_index
        );
        return Err(ProgramError::InvalidSeeds);
    }

    try_create_account(
        Invoker::Signer(payer_info.key),
        Invoker::Pda {
            key: &expected_custodied_2z_key,
            signer_seeds: &[
                CUSTODIED_2Z_SEED_PREFIX,
                expected_distribution_key.as_ref(),
                &[custodied_2z_bump],
            ],
        },
        new_2z_custody_token_info.lamports(),
        spl_token::state::Account::LEN,
        &spl_token::ID,
        accounts,
        Some(&rent_sysvar),
    )?;

    // Account 3 must be the 2Z mint.
    let (account_index, spl_2z_mint_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Enforce this account location.
    if spl_2z_mint_info.key != &DOUBLEZERO_MINT {
        msg!("Invalid address for 2Z mint (account {})", account_index);
        return Err(ProgramError::InvalidAccountData);
    }

    // Account 4 must be the SPL Token program.
    let (account_index, spl_token_program_info) =
        try_next_enumerated_account(&mut accounts_iter, Default::default())?;

    // Enforce this account location.
    if spl_token_program_info.key != &spl_token::ID {
        msg!(
            "Invalid address for SPL Token program (account {})",
            account_index
        );
        return Err(ProgramError::InvalidAccountData);
    }

    let initialize_token_account_ix = spl_token::instruction::initialize_account3(
        &spl_token::ID,
        &expected_custodied_2z_key,
        &DOUBLEZERO_MINT,
        &expected_distribution_key,
    )
    .unwrap();

    invoke_signed_unchecked(&initialize_token_account_ix, accounts, &[])?;

    Ok(())
}

fn process_configure_distribution(
    accounts: &[AccountInfo],
    data: ConfigureDistributionData,
) -> ProgramResult {
    msg!("Configure distribution");

    // We expect 3 accounts for this instruction at the following indices:
    // - 0: Program config.
    // - 1: Accountant.
    // - 2: Distribution.
    let mut accounts_iter = accounts.iter().enumerate();

    VerifiedProgramAuthority::try_next_accounts(&mut accounts_iter, Authority::Accountant)?;

    // Account 2 must be the program config account.
    let mut distribution =
        ZeroCopyMutAccount::<Distribution>::try_next_accounts(&mut accounts_iter, Some(&ID))?;

    match data {
        ConfigureDistributionData::SolanaValidatorPayments {
            total_owed,
            merkle_root,
        } => {
            msg!("Set total_solana_validator_payments_owed: {}", total_owed);
            distribution.total_solana_validator_payments_owed = total_owed;

            msg!("Set solana_validator_payments_merkle_root: {}", merkle_root);
            distribution.solana_validator_payments_merkle_root = merkle_root;
        }
        ConfigureDistributionData::ContributorRewards {
            total_contributors,
            merkle_root,
        } => {
            msg!("set total_contributors: {}", total_contributors);
            distribution.total_contributors = total_contributors;

            msg!("Set contributor_rewards_merkle_root: {}", merkle_root);
            distribution.contributor_rewards_merkle_root = merkle_root;
        }
    }

    Ok(())
}

//
// Account info handling.
//

enum Authority {
    Admin,
    Accountant,
}

impl Authority {
    fn try_next_as_authorized_account<'b, 'c>(
        &self,
        accounts_iter: &mut EnumeratedAccountInfoIter<'b, 'c>,
        program_config: &ProgramConfig,
    ) -> Result<(usize, &'b AccountInfo<'c>), ProgramError> {
        let (index, authority_info) = try_next_enumerated_account(
            accounts_iter,
            NextAccountOptions {
                must_be_signer: true,
                ..Default::default()
            },
        )?;

        match self {
            Authority::Admin => {
                if authority_info.key != &program_config.admin_key {
                    msg!("Unauthorized admin (account {})", index);
                    return Err(ProgramError::InvalidAccountData);
                }
            }
            Authority::Accountant => {
                if authority_info.key != &program_config.accountant_key {
                    msg!("Unauthorized accountant (account {})", index);
                    return Err(ProgramError::InvalidAccountData);
                }
            }
        }

        Ok((index, authority_info))
    }
}

impl std::fmt::Display for Authority {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Authority::Admin => write!(f, "Admin"),
            Authority::Accountant => write!(f, "Accountant"),
        }
    }
}

struct VerifiedProgramAuthority<'a, 'b> {
    _program_config: ZeroCopyAccount<'a, 'b, ProgramConfig>,
    _authority: (usize, &'a AccountInfo<'b>),
}

impl<'a, 'b> TryNextAccounts<'a, 'b, Authority> for VerifiedProgramAuthority<'a, 'b> {
    fn try_next_accounts(
        accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
        authority: Authority,
    ) -> Result<Self, ProgramError> {
        // Index == 0.
        let program_config = ZeroCopyAccount::try_next_accounts(accounts_iter, Some(&ID))?;

        // Index == 1.
        let (index, authority_info) =
            authority.try_next_as_authorized_account(accounts_iter, &program_config.data)?;

        Ok(Self {
            _program_config: program_config,
            _authority: (index, authority_info),
        })
    }
}

struct VerifiedProgramAuthorityMut<'a, 'b> {
    program_config: ZeroCopyMutAccount<'a, 'b, ProgramConfig>,
    _authority: (usize, &'a AccountInfo<'b>),
}

impl<'a, 'b> TryNextAccounts<'a, 'b, Authority> for VerifiedProgramAuthorityMut<'a, 'b> {
    fn try_next_accounts(
        accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
        authority: Authority,
    ) -> Result<Self, ProgramError> {
        // Index == 0.
        let program_config = ZeroCopyMutAccount::try_next_accounts(accounts_iter, Some(&ID))?;

        // Index == 1.
        let (index, authority_info) =
            authority.try_next_as_authorized_account(accounts_iter, &program_config.data)?;

        Ok(Self {
            program_config,
            _authority: (index, authority_info),
        })
    }
}
