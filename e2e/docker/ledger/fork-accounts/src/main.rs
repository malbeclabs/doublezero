use std::{env, fs, path::Path, process};

use base64::{engine::general_purpose::STANDARD as BASE64, Engine};
use doublezero_sdk::{AccountType, GlobalState};
use serde::{Deserialize, Serialize};
use solana_sdk::pubkey::Pubkey;

fn main() {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 {
        eprintln!("Usage: fork-accounts <fetch|patch-globalstate> [args...]");
        process::exit(1);
    }

    match args[1].as_str() {
        "fetch" => cmd_fetch(&args[2..]),
        "patch-globalstate" => cmd_patch_globalstate(&args[2..]),
        _ => {
            eprintln!("Unknown command: {}", args[1]);
            process::exit(1);
        }
    }
}

/// Account data format used by solana-test-validator's --account-dir.
#[derive(Serialize, Deserialize)]
struct AccountFile {
    pubkey: String,
    account: AccountData,
}

#[derive(Serialize, Deserialize)]
struct AccountData {
    lamports: u64,
    data: (String, String), // (base64_data, "base64")
    owner: String,
    executable: bool,
    #[serde(rename = "rentEpoch")]
    rent_epoch: u64,
    space: u64,
}

/// RPC response types for getProgramAccounts.
#[derive(Deserialize)]
struct RpcResponse {
    result: Option<Vec<RpcAccount>>,
    error: Option<serde_json::Value>,
}

#[derive(Deserialize)]
struct RpcAccount {
    pubkey: String,
    account: RpcAccountData,
}

#[derive(Deserialize)]
struct RpcAccountData {
    lamports: u64,
    data: (String, String),
    owner: String,
    executable: bool,
    #[serde(rename = "rentEpoch")]
    rent_epoch: u64,
    space: u64,
}

/// Fetch all accounts owned by one or more programs and write them as JSON files.
/// Program IDs can be comma-separated.
fn cmd_fetch(args: &[String]) {
    if args.len() < 3 {
        eprintln!("Usage: fork-accounts fetch <rpc-url> <program-ids> <output-dir>");
        process::exit(1);
    }
    let rpc_url = &args[0];
    let program_ids_str = &args[1];
    let output_dir = &args[2];

    fs::create_dir_all(output_dir).expect("failed to create output directory");

    for program_id in program_ids_str.split(',') {
        let program_id = program_id.trim();
        if program_id.is_empty() {
            continue;
        }

        eprintln!("==> Fetching accounts for program {program_id} from {rpc_url}");

        let body = serde_json::json!({
            "jsonrpc": "2.0",
            "id": 1,
            "method": "getProgramAccounts",
            "params": [program_id, {"encoding": "base64"}]
        });

        let response: RpcResponse = ureq::post(rpc_url)
            .set("Content-Type", "application/json")
            .send_json(&body)
            .expect("RPC request failed")
            .into_json()
            .expect("failed to parse RPC response");

        if let Some(err) = response.error {
            eprintln!("RPC error: {err}");
            process::exit(1);
        }

        let accounts = response.result.unwrap_or_default();
        for entry in &accounts {
            let file = AccountFile {
                pubkey: entry.pubkey.clone(),
                account: AccountData {
                    lamports: entry.account.lamports,
                    data: entry.account.data.clone(),
                    owner: entry.account.owner.clone(),
                    executable: entry.account.executable,
                    rent_epoch: entry.account.rent_epoch,
                    space: entry.account.space,
                },
            };
            let path = Path::new(output_dir).join(format!("{}.json", entry.pubkey));
            let json = serde_json::to_string(&file).expect("failed to serialize account");
            fs::write(&path, json).expect("failed to write account file");
        }

        eprintln!("Wrote {} accounts for program {program_id}", accounts.len());
    }
}

/// Patch a GlobalState account's foundation_allowlist and activator_authority_pk.
fn cmd_patch_globalstate(args: &[String]) {
    if args.len() < 2 {
        eprintln!("Usage: fork-accounts patch-globalstate <accounts-dir> <authority-pubkey>");
        process::exit(1);
    }
    let accounts_dir = &args[0];
    let authority_b58 = &args[1];

    let authority: Pubkey = authority_b58.parse().unwrap_or_else(|e| {
        eprintln!("Invalid authority pubkey '{authority_b58}': {e}");
        process::exit(1);
    });

    eprintln!("==> Patching GlobalState foundation_allowlist with {authority_b58}");

    let mut patched = false;
    for entry in fs::read_dir(accounts_dir).expect("failed to read accounts directory") {
        let entry = entry.expect("failed to read directory entry");
        let path = entry.path();
        if path.extension().and_then(|s| s.to_str()) != Some("json") {
            continue;
        }

        let content = fs::read_to_string(&path).expect("failed to read account file");
        let mut file: AccountFile =
            serde_json::from_str(&content).expect("failed to parse account file");

        // Decode the account data.
        let data = BASE64
            .decode(&file.account.data.0)
            .expect("failed to decode base64 data");

        // GlobalState has account_type == 1 as first byte.
        if data.is_empty() || data[0] != AccountType::GlobalState as u8 {
            continue;
        }

        // Deserialize using the SDK's TryFrom<&[u8]>.
        let mut global_state = GlobalState::try_from(data.as_slice()).unwrap_or_else(|e| {
            eprintln!(
                "Failed to deserialize GlobalState from {}: {e}",
                path.display()
            );
            process::exit(1);
        });

        // Add authority to foundation_allowlist.
        let prev_len = global_state.foundation_allowlist.len();
        global_state.foundation_allowlist.push(authority);

        // Set activator_authority_pk to the same authority.
        global_state.activator_authority_pk = authority;

        // Re-serialize and write back.
        let new_data = borsh::to_vec(&global_state).unwrap_or_else(|e| {
            eprintln!("Failed to serialize GlobalState: {e}");
            process::exit(1);
        });

        file.account.data.0 = BASE64.encode(&new_data);
        file.account.space = new_data.len() as u64;

        let json = serde_json::to_string(&file).expect("failed to serialize account file");
        fs::write(&path, json).expect("failed to write account file");

        eprintln!(
            "Patched GlobalState account {}: added authority to foundation_allowlist (now {} entries) and set activator_authority_pk",
            path.file_stem().unwrap().to_str().unwrap(),
            prev_len + 1
        );
        patched = true;
        break;
    }

    if !patched {
        eprintln!("WARNING: No GlobalState account found to patch");
    }
}
