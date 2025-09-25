use anyhow::{bail, Result};
use clap::{Args, Subcommand};
use doublezero_passport::{
    instruction::{
        account::{ConfigureProgramAccounts, InitializeProgramAccounts, SetAdminAccounts},
        PassportInstructionData, ProgramConfiguration, ProgramFlagConfiguration,
    },
    state::ProgramConfig,
    ID,
};
use doublezero_program_tools::{get_program_data_address, instruction::try_build_instruction};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, Wallet};
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction, instruction::Instruction, pubkey::Pubkey,
};

#[derive(Debug, Subcommand)]
pub enum PassportAdminSubCommand {
    /// Initialize and set admin to upgrade authority.
    Initialize {
        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Set admin to a specified key.
    SetAdmin {
        admin_key: Pubkey,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Configure the program.
    Configure {
        #[command(flatten)]
        configure_options: ConfigurePassportOptions,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },
}

impl PassportAdminSubCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            PassportAdminSubCommand::Initialize {
                solana_payer_options,
            } => execute_initialize_program(solana_payer_options).await,
            PassportAdminSubCommand::SetAdmin {
                admin_key,
                solana_payer_options,
            } => execute_set_admin(admin_key, solana_payer_options).await,
            PassportAdminSubCommand::Configure {
                configure_options,
                solana_payer_options,
            } => execute_configure_program(configure_options, solana_payer_options).await,
        }
    }
}

#[derive(Debug, Args)]
pub struct ConfigurePassportOptions {
    // Flags.
    //
    /// Whether to pause the program. Cannot be used with --unpause.
    #[arg(long)]
    pause: bool,

    /// Whether to unpause the program. Cannot be used with --pause.
    #[arg(long)]
    unpause: bool,

    /// Set the DoubleZero Ledger sentinel key.
    #[arg(long, value_name = "PUBKEY")]
    sentinel: Option<Pubkey>,

    /// Set the access request deposit lamports.
    #[arg(long, value_name = "LAMPORTS")]
    access_request_deposit: Option<u64>,

    /// Set the access request fee lamports.
    #[arg(long, value_name = "LAMPORTS")]
    access_fee: Option<u64>,

    /// Set number of Solana validator backup IDs allowed per staked node.
    #[arg(long, value_name = "NUMBER")]
    solana_validator_backup_ids_limit: Option<u16>,
}

//
// PassportAdminSubCommand::Initialize.
//

pub async fn execute_initialize_program(solana_payer_options: SolanaPayerOptions) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;

    let wallet_key = wallet.pubkey();

    let initialize_program_ix = try_build_instruction(
        &ID,
        InitializeProgramAccounts::new(&wallet_key),
        &PassportInstructionData::InitializeProgram,
    )?;

    let set_admin_ix = try_build_instruction(
        &ID,
        SetAdminAccounts::new(&ID, &wallet_key),
        &PassportInstructionData::SetAdmin(wallet_key),
    )?;

    // Precisely calculate the amount of compute units needed for the instructions.
    // There should be ~5k CU buffer with this base.
    let mut compute_unit_limit = 16_000;

    let (_, bump) = ProgramConfig::find_address();
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let (_, bump) = get_program_data_address(&ID);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    let mut instructions = vec![
        initialize_program_ix,
        set_admin_ix,
        ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
    ];

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Initialized Passport program: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

//
// PassportAdminSubCommand::SetAdmin.
//

pub async fn execute_set_admin(
    admin_key: Pubkey,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;

    let wallet_key = wallet.pubkey();

    let set_admin_ix = try_build_instruction(
        &ID,
        SetAdminAccounts::new(&ID, &wallet_key),
        &PassportInstructionData::SetAdmin(admin_key),
    )?;

    // Precisely calculate the amount of compute units needed for the instructions.
    // There should be ~3k CU buffer with this base.
    let compute_unit_limit = 10_000;

    let mut instructions = vec![
        set_admin_ix,
        ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
    ];

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Set Passport program admin: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

//
// PassportAdminSubCommand::Configure.
//

pub async fn execute_configure_program(
    configure_options: ConfigurePassportOptions,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let ConfigurePassportOptions {
        pause,
        unpause,
        sentinel,
        access_request_deposit,
        access_fee,
        solana_validator_backup_ids_limit,
    } = configure_options;

    let wallet = Wallet::try_from(solana_payer_options)?;
    let wallet_key = wallet.pubkey();

    let mut instructions = vec![];
    let mut compute_unit_limit = 5_000;

    match (pause, unpause) {
        (true, true) => {
            bail!("Cannot use both --pause and --unpause at the same time");
        }
        (true, false) => {
            let configure_program_ix = try_build_configure_program_instruction(
                &wallet_key,
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(true)),
            )?;
            instructions.push(configure_program_ix);
            compute_unit_limit += 2_000;
        }
        (false, true) => {
            let configure_program_ix = try_build_configure_program_instruction(
                &wallet_key,
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            )?;
            instructions.push(configure_program_ix);
            compute_unit_limit += 2_000;
        }
        (false, false) => {}
    }

    if let Some(sentinel_key) = sentinel {
        let configure_program_ix = try_build_configure_program_instruction(
            &wallet_key,
            ProgramConfiguration::DoubleZeroLedgerSentinel(sentinel_key),
        )?;
        instructions.push(configure_program_ix);
        compute_unit_limit += 3_000;
    }

    // Both access need to be specified.
    match (access_request_deposit, access_fee) {
        (Some(request_deposit_lamports), Some(request_fee_lamports)) => {
            let configure_program_ix = try_build_configure_program_instruction(
                &wallet_key,
                ProgramConfiguration::AccessRequestDeposit {
                    request_deposit_lamports,
                    request_fee_lamports,
                },
            )?;
            instructions.push(configure_program_ix);
            compute_unit_limit += 2_500;
        }
        (None, None) => {}
        _ => {
            bail!("Access request deposit and access fee must be specified");
        }
    }

    if let Some(limit) = solana_validator_backup_ids_limit {
        let configure_program_ix = try_build_configure_program_instruction(
            &wallet_key,
            ProgramConfiguration::SolanaValidatorBackupIdsLimit(limit),
        )?;
        instructions.push(configure_program_ix);
        compute_unit_limit += 2_000;
    }

    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let Some(tx_sig) = tx_sig {
        println!("Configured Passport program: {tx_sig}");

        wallet.print_verbose_output(&[tx_sig]).await?;
    }

    Ok(())
}

//

fn try_build_configure_program_instruction(
    admin_key: &Pubkey,
    setting: ProgramConfiguration,
) -> Result<Instruction> {
    try_build_instruction(
        &ID,
        ConfigureProgramAccounts::new(admin_key),
        &PassportInstructionData::ConfigureProgram(setting),
    )
    .map_err(Into::into)
}
