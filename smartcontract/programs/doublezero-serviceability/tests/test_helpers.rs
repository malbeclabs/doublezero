use borsh::to_vec;
use doublezero_serviceability::{
    entrypoint::process_instruction,
    instructions::*,
    pda::{
        get_globalconfig_pda, get_globalstate_pda, get_program_config_pda,
        get_resource_extension_pda,
    },
    processors::globalconfig::set::SetGlobalConfigArgs,
    resource::ResourceType,
    state::{
        accountdata::AccountData, accounttype::AccountType, device::Device,
        globalstate::GlobalState, resource_extension::ResourceExtensionOwned,
    },
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    system_instruction, system_program,
    transaction::Transaction,
};
use std::any::type_name;

// Use a fixed byte array to create a constant Keypair for testing
// This is safe for tests only; never use hardcoded keys in production!
#[allow(dead_code)]
pub const TEST_PAYER_BYTES: [u8; 64] = [
    169, 191, 120, 114, 135, 172, 221, 186, 245, 154, 139, 162, 103, 229, 16, 1, 170, 160, 159, 47,
    224, 60, 179, 71, 245, 255, 116, 238, 144, 208, 19, 89, 13, 59, 115, 1, 186, 171, 180, 37, 165,
    122, 75, 128, 161, 44, 98, 93, 190, 124, 25, 3, 175, 219, 173, 30, 195, 19, 111, 203, 78, 54,
    241, 90,
];

#[allow(dead_code)]
pub fn test_payer() -> Keypair {
    Keypair::from_bytes(&TEST_PAYER_BYTES).unwrap()
}

#[allow(dead_code)]
pub async fn init_test() -> (BanksClient, Pubkey, Keypair, solana_program::hash::Hash) {
    let program_id = Pubkey::new_unique();

    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    transfer(
        &mut banks_client,
        &payer,
        &test_payer().pubkey(),
        100_000_000,
    )
    .await;

    (banks_client, program_id, payer, recent_blockhash)
}

#[allow(dead_code)]
pub async fn get_globalstate(
    banks_client: &mut BanksClient,
    globalstate_pubkey: Pubkey,
) -> GlobalState {
    match banks_client.get_account(globalstate_pubkey).await {
        Ok(account) => match account {
            Some(account_data) => {
                let globalstate = GlobalState::try_from(&account_data.data[..]).unwrap();
                assert_eq!(globalstate.account_type, AccountType::GlobalState);

                println!("⬅️  Read {globalstate:?}");

                globalstate
            }
            None => panic!("GlobalState account not found"),
        },
        Err(err) => panic!("GlobalState account not found: {err:?}"),
    }
}

#[allow(dead_code)]
pub fn get_type_name<T>() -> String {
    let full_type_name = type_name::<T>();
    if let Some(last_name) = full_type_name.rsplit("::").next() {
        return last_name.to_string();
    }

    "".to_string()
}

#[allow(dead_code)]
pub async fn get_account_data(
    banks_client: &mut BanksClient,
    pubkey: Pubkey,
) -> Option<AccountData> {
    print!("Read: ");

    match banks_client.get_account(pubkey).await {
        Ok(account) => match account {
            Some(account_data) => match AccountData::try_from(&account_data.data[..]) {
                Ok(object) => {
                    println!("{object:?}");
                    Some(object)
                }
                Err(err) => {
                    println!("{account_data:?}");
                    println!("Failed to deserialize account data: {err:?}");
                    None
                }
            },
            None => None,
        },
        Err(err) => panic!("account not found: {err:?}"),
    }
}

#[allow(dead_code)]
pub async fn transfer(
    banks_client: &mut BanksClient,
    source: &Keypair,
    destination: &Pubkey,
    lamports: u64,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let transfer_ix = system_instruction::transfer(&source.pubkey(), destination, lamports);
    let mut tx = Transaction::new_with_payer(&[transfer_ix], Some(&source.pubkey()));
    tx.sign(&[&source], recent_blockhash);
    banks_client.process_transaction(tx).await.unwrap();
}

pub async fn execute_transaction(
    banks_client: &mut BanksClient,
    _recent_blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &Keypair,
) {
    println!("➡️  Transaction {instruction:?}");

    // Test with a diferent signer
    execute_transaction_tester(
        banks_client,
        program_id,
        instruction.clone(),
        accounts.clone(),
        payer,
    )
    .await
    .unwrap();

    // Execute the transaction with the real payer
    let recent_blockhash = banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get latest blockhash");
    let mut transaction = create_transaction(program_id, &instruction, &accounts, payer);
    transaction.try_sign(&[&payer], recent_blockhash).unwrap();
    if let Err(e) = banks_client.process_transaction(transaction).await {
        panic!("Transaction failed for {instruction:?}: {e:?}");
    }

    println!("✅");
}

async fn execute_transaction_tester(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &Keypair,
) -> Result<(), BanksClientError> {
    let test_payer = Pubkey::new_unique();

    let recent_blockhash = banks_client
        .get_latest_blockhash()
        .await
        .expect("Failed to get latest blockhash");

    println!("➡️  Testing a transaction without the payer signing it...");
    let mut transaction = Transaction::new_with_payer(
        &[Instruction::new_with_bytes(
            program_id,
            &to_vec(&instruction).unwrap(),
            [
                accounts.clone(),
                vec![
                    AccountMeta::new(test_payer, false),
                    AccountMeta::new(system_program::id(), false),
                ],
            ]
            .concat(),
        )],
        Some(&payer.pubkey()),
    );
    transaction.try_sign(&[&payer], recent_blockhash).unwrap();
    let res = banks_client.process_transaction(transaction).await;

    assert!(
        res.is_err(),
        "Transaction should have failed (different signer)"
    );

    println!("🟢 Testing invalid payer - passed");

    Ok(())
}

#[allow(dead_code)]
pub async fn try_execute_transaction(
    banks_client: &mut BanksClient,
    recent_blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &Keypair,
) -> Result<(), BanksClientError> {
    print!("➡️  Transaction {instruction:?} ");

    let mut transaction = create_transaction(program_id, &instruction, &accounts, payer);
    transaction.try_sign(&[&payer], recent_blockhash).unwrap();
    banks_client.process_transaction(transaction).await?;

    println!("✅");

    Ok(())
}

pub fn create_transaction(
    program_id: Pubkey,
    instruction: &DoubleZeroInstruction,
    accounts: &Vec<AccountMeta>,
    payer: &Keypair,
) -> Transaction {
    create_transaction_with_extra_accounts(program_id, instruction, accounts, payer, &[])
}

/// Create a transaction with optional extra accounts appended after payer and system_program.
/// This is useful for instructions that have optional accounts at the end (like ResourceExtension).
#[allow(dead_code)]
pub fn create_transaction_with_extra_accounts(
    program_id: Pubkey,
    instruction: &DoubleZeroInstruction,
    accounts: &Vec<AccountMeta>,
    payer: &Keypair,
    extra_accounts: &[AccountMeta],
) -> Transaction {
    Transaction::new_with_payer(
        &[Instruction::new_with_bytes(
            program_id,
            &to_vec(instruction).unwrap(),
            [
                accounts.to_owned(),
                vec![
                    AccountMeta::new(payer.pubkey(), true),
                    AccountMeta::new(system_program::id(), false),
                ],
                extra_accounts.to_vec(),
            ]
            .concat(),
        )],
        Some(&payer.pubkey()),
    )
}

#[allow(dead_code)]
pub async fn get_resource_extension_data(
    banks_client: &mut BanksClient,
    pubkey: Pubkey,
) -> Option<ResourceExtensionOwned> {
    print!("Read ResourceExtension: ");

    match banks_client.get_account(pubkey).await {
        Ok(account) => match account {
            Some(account_data) => match ResourceExtensionOwned::try_from(&account_data.data[..]) {
                Ok(resource) => {
                    println!("{resource}");
                    Some(resource)
                }
                Err(err) => {
                    println!("Failed to deserialize ResourceExtension: {err:?}");
                    None
                }
            },
            None => {
                println!("Account not found");
                None
            }
        },
        Err(err) => panic!("Failed to get account: {err:?}"),
    }
}

#[allow(dead_code)]
pub async fn get_device(banks_client: &mut BanksClient, pubkey: Pubkey) -> Option<Device> {
    print!("Read Device: ");

    match banks_client.get_account(pubkey).await {
        Ok(account) => match account {
            Some(account_data) => match Device::try_from(&account_data.data[..]) {
                Ok(device) => {
                    println!("{device}");
                    Some(device)
                }
                Err(err) => {
                    println!("Failed to deserialize Device: {err:?}");
                    None
                }
            },
            None => {
                println!("Account not found");
                None
            }
        },
        Err(err) => panic!("Failed to get account: {err:?}"),
    }
}

/// Wait for a new blockhash to avoid transaction deduplication
#[allow(dead_code)]
pub async fn wait_for_new_blockhash(banks_client: &mut BanksClient) -> solana_program::hash::Hash {
    let current_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let mut new_blockhash = current_blockhash;
    while new_blockhash == current_blockhash {
        new_blockhash = banks_client.get_latest_blockhash().await.unwrap();
        tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
    }
    new_blockhash
}

/// Setup program with global state and global config initialized.
/// Returns (banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey)
#[allow(dead_code)]
pub async fn setup_program_with_globalconfig() -> (BanksClient, Keypair, Pubkey, Pubkey, Pubkey) {
    let program_id = Pubkey::new_unique();

    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);
    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);

    // Initialize global state
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Set global config with tunnel blocks for resource testing
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.100.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.200.0.0/24".parse().unwrap(),
            multicastgroup_block: "239.0.0.0/24".parse().unwrap(),
            next_bgp_community: None,
        }),
        vec![
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(user_tunnel_block_pda, false),
            AccountMeta::new(multicastgroup_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
        ],
        &payer,
    )
    .await;

    (
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
    )
}
