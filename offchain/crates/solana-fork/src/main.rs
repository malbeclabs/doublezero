use std::{fs, process::Command};

use anyhow::{Result, ensure};
use base64::{Engine, engine::general_purpose::STANDARD as BASE64};
use clap::Parser;
use doublezero_passport::ID as PASSPORT_PROGRAM_ID;
use doublezero_revenue_distribution::{ID as REVENUE_DISTRIBUTION_PROGRAM_ID, env};
use doublezero_solana_client_tools::{
    payer::try_load_keypair,
    rpc::{SolanaConnection, SolanaConnectionOptions},
};
use serde::Serialize;
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig};
use solana_sdk::{account::Account, pubkey::Pubkey, signer::Signer};

const ACCOUNTS_PATH: &str = "forked-accounts";
const TMP_ACCOUNTS_PATH: &str = "forked-accounts.tmp";

#[derive(Serialize)]
struct WrittenAccountInfo {
    lamports: u64,
    data: (String, String),
    owner: String,
    executable: bool,
    #[serde(rename = "rentEpoch")]
    rent_epoch: u64,
    space: usize,
}

#[derive(Serialize)]
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

    /// Overwrite existing accounts without prompting.
    #[arg(long)]
    overwrite_accounts: bool,

    #[command(flatten)]
    solana_connection_options: SolanaConnectionOptions,
}

#[tokio::main]
async fn main() -> Result<()> {
    let Args {
        upgrade_authority: upgrade_authority_key,
        overwrite_accounts: should_overwrite_accounts,
        solana_connection_options,
    } = Args::parse();

    let mut connection = SolanaConnection::try_from(solana_connection_options)?;
    connection.cache_if_mainnet().await?;

    // Get upgrade authority from argument or default keypair.
    let upgrade_authority_key = match upgrade_authority_key {
        Some(key) => key,
        None => {
            let keypair = try_load_keypair(None)?;
            keypair.pubkey()
        }
    };

    if fs::metadata(ACCOUNTS_PATH).is_err() || should_overwrite_accounts {
        fs::create_dir_all(TMP_ACCOUNTS_PATH)?;

        let fetch_result = async {
            // Fetch 2Z mint account.

            let token_2z_mint_key = if connection.is_mainnet {
                env::mainnet::DOUBLEZERO_MINT_KEY
            } else {
                env::development::DOUBLEZERO_MINT_KEY
            };

            let mint_account = connection.get_account(&token_2z_mint_key).await?;
            write_account_to_file(&token_2z_mint_key, &mint_account, TMP_ACCOUNTS_PATH)?;
            println!("Wrote 2Z mint account to {TMP_ACCOUNTS_PATH}/");

            // Now fetch program accounts.

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
                &connection,
                &REVENUE_DISTRIBUTION_PROGRAM_ID,
                "Revenue Distribution",
                TMP_ACCOUNTS_PATH,
                &config,
            )
            .await?;

            try_fetch_and_write_program_accounts(
                &connection,
                &PASSPORT_PROGRAM_ID,
                "Passport",
                TMP_ACCOUNTS_PATH,
                &config,
            )
            .await?;

            // Dump programs.

            try_dump_program(
                &connection,
                &REVENUE_DISTRIBUTION_PROGRAM_ID,
                "Revenue Distribution",
                &format!("{TMP_ACCOUNTS_PATH}/revenue_distribution.so"),
            )?;

            try_dump_program(
                &connection,
                &PASSPORT_PROGRAM_ID,
                "Passport",
                &format!("{TMP_ACCOUNTS_PATH}/passport.so"),
            )?;

            Ok(())
        };

        match fetch_result.await {
            Ok(_) => {
                // Remove existing accounts directory if it exists, then rename
                // temporary directory to final location.
                if fs::metadata(ACCOUNTS_PATH).is_ok() {
                    fs::remove_dir_all(ACCOUNTS_PATH)?;
                }
                fs::rename(TMP_ACCOUNTS_PATH, ACCOUNTS_PATH)?;
            }
            Err(e) => {
                fs::remove_dir_all(TMP_ACCOUNTS_PATH)?;
                return Err(e);
            }
        }
    } else {
        eprintln!(
            "Directory {ACCOUNTS_PATH} already exists. Use --overwrite-accounts to force a new fork"
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

    let status = Command::new("solana-test-validator")
        .arg("--url")
        .arg(connection.rpc_client.url())
        .arg("--account-dir")
        .arg(ACCOUNTS_PATH)
        .arg("--reset")
        .arg("--upgradeable-program")
        .arg(REVENUE_DISTRIBUTION_PROGRAM_ID.to_string())
        .arg(format!("{ACCOUNTS_PATH}/revenue_distribution.so"))
        .arg(upgrade_authority_key.to_string())
        .arg("--upgradeable-program")
        .arg(PASSPORT_PROGRAM_ID.to_string())
        .arg(format!("{ACCOUNTS_PATH}/passport.so"))
        .arg(upgrade_authority_key.to_string())
        .status()?;

    ensure!(
        status.success(),
        "solana-test-validator exited with status: {status}"
    );

    Ok(())
}

//

fn write_account_to_file(
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

    let json = serde_json::to_string_pretty(&wrapper)?;
    let file_path = format!("{accounts_dir}/{account_key}.json");
    fs::write(&file_path, json)?;

    Ok(())
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
        write_account_to_file(key, account, accounts_dir)?;
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
        .arg(connection.rpc_client.url())
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
