use std::{fs, process::Command};

use anyhow::{Context, Result, ensure};
use base64::{Engine, engine::general_purpose::STANDARD as BASE64};
use clap::Parser;
use doublezero_solana_client_tools::{
    payer::try_load_keypair,
    rpc::{SolanaConnection, SolanaConnectionOptions},
};
use doublezero_solana_sdk::{
    NetworkEnvironment, PrecomputedDiscriminator, environment_2z_token_mint_key,
    passport::{ID as PASSPORT_PROGRAM_ID, state::ProgramConfig as PassportProgramConfig},
    revenue_distribution::{
        self, ID as REVENUE_DISTRIBUTION_PROGRAM_ID,
        state::{Distribution, Journal, ProgramConfig as RevenueDistributionProgramConfig},
        types::DoubleZeroEpoch,
    },
    sol_conversion::{
        ID as SOL_CONVERSION_PROGRAM_ID, state::ProgramState as SolConversionProgramState,
    },
    zero_copy,
};
use serde::{Deserialize, Serialize};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig};
use solana_sdk::{account::Account, program_pack::Pack, pubkey::Pubkey, signer::Signer};

const ACCOUNTS_PATH: &str = "forked-accounts";
const TMP_ACCOUNTS_PATH: &str = "forked-accounts.tmp";

#[derive(Deserialize, Serialize)]
struct WrittenAccountInfo {
    lamports: u64,
    data: (String, String),
    owner: String,
    executable: bool,
    #[serde(rename = "rentEpoch")]
    rent_epoch: u64,
    space: usize,
}

#[derive(Deserialize, Serialize)]
struct WrittenAccount {
    pubkey: String,
    account: WrittenAccountInfo,
}

#[derive(Parser, Debug)]
#[command(term_width = 0)]
#[command(version = option_env!("BUILD_VERSION").unwrap_or(env!("CARGO_PKG_VERSION")))]
#[command(about = "Solana local validator fork of DoubleZero programs", long_about = None)]
struct Args {
    /// Upgrade authority for the program (defaults to pubkey from Solana config
    /// keypair).
    #[arg(long, value_name = "PUBKEY")]
    upgrade_authority: Option<Pubkey>,

    /// Reset accounts by fetching fresh data, overwriting existing accounts.
    #[arg(long)]
    reset: bool,

    /// Hidden god-mode command, which will overwrite admin and other
    /// authorities with the upgrade authority.
    #[arg(long, hide = true)]
    god_mode: bool,

    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,
}

#[tokio::main]
async fn main() -> Result<()> {
    let Args {
        upgrade_authority: upgrade_authority_key,
        reset: should_reset,
        god_mode: should_god_mode,
        solana_connection_options,
    } = Args::parse();

    let connection = SolanaConnection::from(solana_connection_options);
    let network_env = connection.try_network_environment().await?;

    // Get upgrade authority from argument or default keypair.
    let upgrade_authority_key = match upgrade_authority_key {
        Some(key) => key,
        None => {
            let keypair = try_load_keypair(None)?;
            keypair.pubkey()
        }
    };

    // Warn if god mode is enabled but reset is not.
    if should_god_mode && !should_reset {
        eprintln!(
            "Warning: --god-mode was passed but --reset was not. God mode will not apply without resetting accounts"
        );
    }

    if should_reset {
        // Clean up any leftover temporary directory from previous failed runs.
        if fs::metadata(TMP_ACCOUNTS_PATH).is_ok() {
            fs::remove_dir_all(TMP_ACCOUNTS_PATH)?;
        }

        // Remove existing accounts directory if it exists.
        if fs::metadata(ACCOUNTS_PATH).is_ok() {
            fs::remove_dir_all(ACCOUNTS_PATH)?;
        }

        fs::create_dir_all(TMP_ACCOUNTS_PATH)?;

        match try_fetch_and_write_accounts(
            &connection,
            network_env,
            upgrade_authority_key,
            should_god_mode,
        )
        .await
        {
            Ok(_) => {
                // Rename temporary directory to final location.
                fs::rename(TMP_ACCOUNTS_PATH, ACCOUNTS_PATH)?;
            }
            Err(e) => {
                fs::remove_dir_all(TMP_ACCOUNTS_PATH)?;
                return Err(e);
            }
        }
    } else {
        // Ensure ACCOUNTS_PATH exists when not resetting.
        ensure!(
            fs::metadata(ACCOUNTS_PATH).is_ok(),
            "Directory {ACCOUNTS_PATH} does not exist. Run with --reset to fetch accounts from the network"
        );
    }

    // Check if solana-test-validator is available.
    let check = Command::new("which")
        .arg("solana-test-validator")
        .output()?;

    ensure!(
        check.status.success(),
        "solana-test-validator not found. Please install Solana CLI tools"
    );

    let mut command = Command::new("solana-test-validator");
    command
        .arg("--url")
        .arg(connection.url())
        .arg("--account-dir")
        .arg(ACCOUNTS_PATH)
        .arg("--upgradeable-program")
        .arg(REVENUE_DISTRIBUTION_PROGRAM_ID.to_string())
        .arg(format!("{ACCOUNTS_PATH}/revenue_distribution.so"))
        .arg(upgrade_authority_key.to_string())
        .arg("--upgradeable-program")
        .arg(PASSPORT_PROGRAM_ID.to_string())
        .arg(format!("{ACCOUNTS_PATH}/passport.so"))
        .arg(upgrade_authority_key.to_string())
        .arg("--upgradeable-program")
        .arg(SOL_CONVERSION_PROGRAM_ID.to_string())
        .arg(format!("{ACCOUNTS_PATH}/sol_conversion.so"))
        .arg(upgrade_authority_key.to_string());

    if should_reset {
        command.arg("--reset");
    }

    let status = command.status()?;

    ensure!(
        status.success(),
        "solana-test-validator exited with status: {status}"
    );

    Ok(())
}

//

async fn try_fetch_and_write_accounts(
    connection: &SolanaConnection,
    network_env: NetworkEnvironment,
    upgrade_authority_key: Pubkey,
    should_god_mode: bool,
) -> Result<()> {
    // Fetch 2Z mint account.

    let token_2z_mint_key = environment_2z_token_mint_key(network_env);

    let mint_account = connection.get_account(&token_2z_mint_key).await?;
    try_write_account_to_file(&token_2z_mint_key, &mint_account, TMP_ACCOUNTS_PATH)?;
    println!("Wrote 2Z mint account to {TMP_ACCOUNTS_PATH}/");

    // Fetch program accounts.

    let config = RpcProgramAccountsConfig {
        filters: None,
        account_config: RpcAccountInfoConfig {
            encoding: Some(UiAccountEncoding::Base64),
            ..Default::default()
        },
        ..Default::default()
    };

    // Fetch all program accounts.

    try_fetch_and_write_program_accounts(
        connection,
        &REVENUE_DISTRIBUTION_PROGRAM_ID,
        "Revenue Distribution",
        TMP_ACCOUNTS_PATH,
        &config,
    )
    .await?;

    try_fetch_and_write_program_accounts(
        connection,
        &PASSPORT_PROGRAM_ID,
        "Passport",
        TMP_ACCOUNTS_PATH,
        &config,
    )
    .await?;

    try_fetch_and_write_program_accounts(
        connection,
        &SOL_CONVERSION_PROGRAM_ID,
        "SOL Conversion",
        TMP_ACCOUNTS_PATH,
        &config,
    )
    .await?;

    // Dump programs.

    try_dump_program(
        connection,
        &REVENUE_DISTRIBUTION_PROGRAM_ID,
        "Revenue Distribution",
        &format!("{TMP_ACCOUNTS_PATH}/revenue_distribution.so"),
    )?;

    try_dump_program(
        connection,
        &PASSPORT_PROGRAM_ID,
        "Passport",
        &format!("{TMP_ACCOUNTS_PATH}/passport.so"),
    )?;

    try_dump_program(
        connection,
        &SOL_CONVERSION_PROGRAM_ID,
        "SOL Conversion",
        &format!("{TMP_ACCOUNTS_PATH}/sol_conversion.so"),
    )?;

    if should_god_mode {
        eprintln!("God mode enabled");

        try_modify_zero_copy_account::<RevenueDistributionProgramConfig>(
            &RevenueDistributionProgramConfig::find_address().0,
            TMP_ACCOUNTS_PATH,
            |config| {
                config.admin_key = upgrade_authority_key;
                config.debt_accountant_key = upgrade_authority_key;
                config.rewards_accountant_key = upgrade_authority_key;
                config.contributor_manager_key = upgrade_authority_key;
                config.last_initialized_distribution_timestamp = Default::default();

                let distribution_params = &mut config.distribution_parameters;
                distribution_params.calculation_grace_period_minutes = 1;
                distribution_params.initialization_grace_period_minutes = 1;
            },
        )?;
        eprintln!("Updated Revenue Distribution config authorities");

        try_modify_zero_copy_account::<PassportProgramConfig>(
            &PassportProgramConfig::find_address().0,
            TMP_ACCOUNTS_PATH,
            |config| {
                config.admin_key = upgrade_authority_key;
                config.sentinel_key = upgrade_authority_key;
            },
        )?;
        eprintln!("Updated Passport config authorities");

        try_modify_borsh_account::<SolConversionProgramState>(
            &SolConversionProgramState::find_address().0,
            TMP_ACCOUNTS_PATH,
            |config| {
                config.admin_key = upgrade_authority_key;
                config.last_trade_slot = 0;
                config.deny_list_authority = upgrade_authority_key;
            },
        )?;
        eprintln!("Updated SOL Conversion config authorities");

        // Override mint authority.

        let mint_path = format!("{TMP_ACCOUNTS_PATH}/{token_2z_mint_key}.json");
        let mint_json = fs::read_to_string(&mint_path)?;
        let mut mint_wrapper = serde_json::from_str::<WrittenAccount>(&mint_json)?;
        let mut mint_data = BASE64.decode(&mint_wrapper.account.data.0)?;

        let mut mint = spl_token::state::Mint::unpack(&mint_data)?;
        mint.mint_authority = upgrade_authority_key.into();

        spl_token::state::Mint::pack(mint, &mut mint_data)?;
        mint_wrapper.account.data.0 = BASE64.encode(&mint_data);
        try_write_wrapped_account_to_file(&token_2z_mint_key, &mint_wrapper, TMP_ACCOUNTS_PATH)?;
    }

    // Fetch various 2Z Token PDAs.

    let mut token_pda_keys = Vec::new();

    let (revenue_distribution_config_key, _) = RevenueDistributionProgramConfig::find_address();
    token_pda_keys.push(
        revenue_distribution::state::find_2z_token_pda_address(&revenue_distribution_config_key).0,
    );

    let (swap_authority_key, _) = revenue_distribution::state::find_swap_authority_address();
    token_pda_keys
        .push(revenue_distribution::state::find_2z_token_pda_address(&swap_authority_key).0);

    let (journal_key, _) = Journal::find_address();
    token_pda_keys.push(revenue_distribution::state::find_2z_token_pda_address(&journal_key).0);

    // For existing distributions, fetch the 2Z token PDAs. Read the
    // Revenue Distribution config account file to deserialize the data
    // and read the next completed DZ epoch.
    let (_, revenue_distribution_config, _) =
        try_read_zero_copy_account::<RevenueDistributionProgramConfig>(
            &revenue_distribution_config_key,
            TMP_ACCOUNTS_PATH,
        )?;

    for epoch in 0..revenue_distribution_config.next_completed_dz_epoch.value() {
        let (distribution_key, _) = Distribution::find_address(DoubleZeroEpoch::new(epoch));
        token_pda_keys
            .push(revenue_distribution::state::find_2z_token_pda_address(&distribution_key).0);
    }

    // Fetch all 2Z token PDA accounts, chunking 100 accounts at a time.
    for token_pda_keys_chunk in token_pda_keys.chunks(100) {
        let token_accounts = connection
            .get_multiple_accounts(token_pda_keys_chunk)
            .await?;
        for (key, token_account) in token_pda_keys_chunk.iter().zip(token_accounts) {
            let account = token_account
                .as_ref()
                .with_context(|| format!("Account does not exist: {}", key))?;
            try_write_account_to_file(key, account, TMP_ACCOUNTS_PATH)?;
        }
    }

    println!(
        "Wrote {} 2Z token PDA accounts to {TMP_ACCOUNTS_PATH}/",
        token_pda_keys.len()
    );

    Ok(())
}

fn try_read_zero_copy_account<T>(
    account_key: &Pubkey,
    accounts_dir: &str,
) -> Result<(WrittenAccount, Box<T>, Vec<u8>)>
where
    T: PrecomputedDiscriminator + bytemuck::Pod,
{
    let path = format!("{accounts_dir}/{account_key}.json");
    let json = fs::read_to_string(&path)?;
    let wrapper = serde_json::from_str::<WrittenAccount>(&json)?;
    let data = BASE64.decode(&wrapper.account.data.0)?;

    let (mucked_data, remaining_data) =
        zero_copy::checked_from_bytes_with_discriminator::<T>(&data)
            .map(|data| (Box::new(*data.0), data.1))
            .unwrap();

    Ok((wrapper, mucked_data, remaining_data.to_vec()))
}

fn try_modify_zero_copy_account<T>(
    account_key: &Pubkey,
    accounts_dir: &str,
    modify_fn: impl FnOnce(&mut T),
) -> Result<()>
where
    T: PrecomputedDiscriminator + bytemuck::Pod,
{
    let (wrapper, mut mucked_data, remaining_data) =
        try_read_zero_copy_account::<T>(account_key, accounts_dir)?;

    modify_fn(&mut mucked_data);

    let mut modified_data = Vec::with_capacity(zero_copy::data_end::<T>() + remaining_data.len());
    modified_data.extend_from_slice(T::discriminator_slice());
    modified_data.extend_from_slice(bytemuck::bytes_of(&*mucked_data));
    modified_data.extend_from_slice(&remaining_data);

    let modified_account = Account {
        lamports: wrapper.account.lamports,
        data: modified_data,
        owner: wrapper.account.owner.parse()?,
        executable: wrapper.account.executable,
        rent_epoch: wrapper.account.rent_epoch,
    };

    try_write_account_to_file(account_key, &modified_account, accounts_dir)
}

fn try_read_borsh_account<T>(
    account_key: &Pubkey,
    accounts_dir: &str,
) -> Result<(WrittenAccount, Box<T>)>
where
    T: PrecomputedDiscriminator + borsh::BorshDeserialize,
{
    let path = format!("{accounts_dir}/{account_key}.json");
    let json = fs::read_to_string(&path)?;
    let wrapper = serde_json::from_str::<WrittenAccount>(&json)?;
    let data = BASE64.decode(&wrapper.account.data.0)?;

    ensure!(
        data.len() > 8 && &data[..8] == T::discriminator_slice(),
        "Invalid discriminator for account: {account_key}",
    );

    let borshed_data = T::deserialize(&mut &data[8..]).map(Box::new)?;

    Ok((wrapper, borshed_data))
}

fn try_modify_borsh_account<T>(
    account_key: &Pubkey,
    accounts_dir: &str,
    modify_fn: impl FnOnce(&mut Box<T>),
) -> Result<()>
where
    T: PrecomputedDiscriminator + borsh::BorshDeserialize + borsh::BorshSerialize,
{
    let (wrapper, mut borshed_data) = try_read_borsh_account::<T>(account_key, accounts_dir)?;

    modify_fn(&mut borshed_data);

    let serialized_data = borsh::to_vec(&borshed_data)?;
    let mut modified_data = Vec::with_capacity(8 + serialized_data.len());
    modified_data.extend_from_slice(T::discriminator_slice());
    modified_data.extend_from_slice(&serialized_data);

    let modified_account = Account {
        lamports: wrapper.account.lamports,
        data: modified_data,
        owner: wrapper.account.owner.parse()?,
        executable: wrapper.account.executable,
        rent_epoch: wrapper.account.rent_epoch,
    };

    try_write_account_to_file(account_key, &modified_account, accounts_dir)
}

fn try_write_account_to_file(
    account_key: &Pubkey,
    account: &Account,
    accounts_dir: &str,
) -> Result<()> {
    let wrapper = WrittenAccount {
        pubkey: account_key.to_string(),
        account: WrittenAccountInfo {
            lamports: account.lamports,
            data: (BASE64.encode(&account.data), "base64".to_string()),
            owner: account.owner.to_string(),
            executable: account.executable,
            rent_epoch: account.rent_epoch,
            space: account.data.len(),
        },
    };

    try_write_wrapped_account_to_file(account_key, &wrapper, accounts_dir)
}

fn try_write_wrapped_account_to_file(
    account_key: &Pubkey,
    wrapper: &WrittenAccount,
    accounts_dir: &str,
) -> Result<()> {
    let json = serde_json::to_string_pretty(&wrapper)?;
    let file_path = format!("{accounts_dir}/{account_key}.json");
    fs::write(&file_path, json).map_err(Into::into)
}

async fn try_fetch_and_write_program_accounts(
    connection: &SolanaConnection,
    program_id: &Pubkey,
    program_name: &str,
    accounts_dir: &str,
    config: &RpcProgramAccountsConfig,
) -> Result<usize> {
    let accounts = connection
        .get_program_accounts_with_config(program_id, config.clone())
        .await?;

    for (key, account) in &accounts {
        try_write_account_to_file(key, account, accounts_dir)?;
    }

    println!(
        "Wrote {} {} accounts to {}/",
        accounts.len(),
        program_name,
        accounts_dir
    );

    Ok(accounts.len())
}

fn try_dump_program(
    connection: &SolanaConnection,
    program_id: &Pubkey,
    program_name: &str,
    output_path: &str,
) -> Result<()> {
    println!("Dumping {} program to {}...", program_name, output_path);

    let dump_status = Command::new("solana")
        .arg("program")
        .arg("dump")
        .arg("--url")
        .arg(connection.url())
        .arg(program_id.to_string())
        .arg(output_path)
        .status()?;

    ensure!(
        dump_status.success(),
        "solana program dump exited with status: {}",
        dump_status
    );

    println!("{} program dumped successfully", program_name);
    Ok(())
}
