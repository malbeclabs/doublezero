#![allow(unused_mut)]

use doublezero_geolocation::{
    entrypoint::process_instruction, instructions::GeolocationInstruction,
    pda::get_program_config_pda, processors::program_config::init::InitProgramConfigArgs,
    serviceability_program_id,
};
use doublezero_serviceability::state::{
    exchange::{Exchange, ExchangeStatus},
    globalstate::GlobalState,
};
use solana_loader_v3_interface::state::UpgradeableLoaderState;
#[allow(deprecated)]
use solana_program::bpf_loader_upgradeable;
use solana_program_test::*;
use solana_sdk::{
    account::Account,
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    signature::Signer,
    transaction::Transaction,
};

/// Builds a bincode-serialized UpgradeableLoaderState::ProgramData account
pub fn build_program_data_account(upgrade_authority: &Pubkey) -> Account {
    let state = UpgradeableLoaderState::ProgramData {
        slot: 0,
        upgrade_authority_address: Some(*upgrade_authority),
    };
    let data = bincode::serde::encode_to_vec(state, bincode::config::legacy()).unwrap();

    Account {
        lamports: 1_000_000_000,
        data,
        owner: bpf_loader_upgradeable::id(),
        executable: false,
        rent_epoch: 0,
    }
}

/// Creates a mock Exchange account owned by the serviceability program
pub fn create_mock_exchange_account(owner: &Pubkey, status: ExchangeStatus) -> Account {
    let exchange = Exchange {
        account_type: doublezero_serviceability::state::accounttype::AccountType::Exchange,
        owner: *owner,
        index: 1,
        bump_seed: 1,
        lat: 52.3676,
        lng: 4.9041,
        bgp_community: 64512,
        unused: 0,
        status,
        code: "test-exchange".to_string(),
        name: "Test Exchange".to_string(),
        reference_count: 0,
        device1_pk: Pubkey::new_unique(),
        device2_pk: Pubkey::new_unique(),
    };

    let data = borsh::to_vec(&exchange).unwrap();
    Account {
        lamports: 1_000_000_000,
        data,
        owner: serviceability_program_id(),
        executable: false,
        rent_epoch: 0,
    }
}

/// Creates a mock GlobalState account for serviceability
pub fn create_mock_globalstate_account(foundation_allowlist: Vec<Pubkey>) -> Account {
    let globalstate = GlobalState {
        account_type: doublezero_serviceability::state::accounttype::AccountType::GlobalState,
        bump_seed: 1,
        account_index: 0,
        foundation_allowlist,
        _device_allowlist: vec![],
        _user_allowlist: vec![],
        activator_authority_pk: Pubkey::new_unique(),
        sentinel_authority_pk: Pubkey::new_unique(),
        contributor_airdrop_lamports: 1_000_000,
        user_airdrop_lamports: 1_000_000,
        health_oracle_pk: Pubkey::new_unique(),
        qa_allowlist: vec![],
        feature_flags: 0,
    };

    let data = borsh::to_vec(&globalstate).unwrap();
    Account {
        lamports: 1_000_000_000,
        data,
        owner: serviceability_program_id(),
        executable: false,
        rent_epoch: 0,
    }
}

/// Sets up a test environment with initialized ProgramConfig
#[allow(dead_code)]
pub async fn setup_test_with_config() -> (
    BanksClient,
    Pubkey,
    tokio::sync::RwLock<solana_sdk::hash::Hash>,
    Pubkey,
) {
    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_geolocation",
        program_id,
        processor!(process_instruction),
    );

    // Add program data account
    let payer_pubkey = Pubkey::new_unique();
    let (program_data_pda, _) =
        Pubkey::find_program_address(&[program_id.as_ref()], &bpf_loader_upgradeable::id());
    program_test.add_account(program_data_pda, build_program_data_account(&payer_pubkey));

    // Add serviceability GlobalState with foundation allowlist
    let serviceability_globalstate_pubkey =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;
    program_test.add_account(
        serviceability_globalstate_pubkey,
        create_mock_globalstate_account(vec![payer_pubkey]),
    );

    let (mut banks_client, payer, recent_blockhash) = program_test.start().await;
    let recent_blockhash = tokio::sync::RwLock::new(recent_blockhash);

    // Initialize ProgramConfig
    let (program_config_pda, _) = get_program_config_pda(&program_id);
    let ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::InitProgramConfig(InitProgramConfigArgs {}),
        vec![
            AccountMeta::new(program_config_pda, false),
            AccountMeta::new_readonly(program_data_pda, false),
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    (banks_client, program_id, recent_blockhash, payer.pubkey())
}

/// Sets up test with config and an exchange
pub async fn setup_test_with_exchange(
    exchange_status: ExchangeStatus,
) -> (
    BanksClient,
    Pubkey,
    tokio::sync::RwLock<solana_sdk::hash::Hash>,
    Pubkey,
    Pubkey,
) {
    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_geolocation",
        program_id,
        processor!(process_instruction),
    );

    // Add program data account
    let payer_pubkey = Pubkey::new_unique();
    let (program_data_pda, _) =
        Pubkey::find_program_address(&[program_id.as_ref()], &bpf_loader_upgradeable::id());
    program_test.add_account(program_data_pda, build_program_data_account(&payer_pubkey));

    // Add serviceability GlobalState with foundation allowlist
    let serviceability_globalstate_pubkey =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;
    program_test.add_account(
        serviceability_globalstate_pubkey,
        create_mock_globalstate_account(vec![payer_pubkey]),
    );

    // Add exchange account
    let exchange_pubkey = Pubkey::new_unique();
    let exchange_account = create_mock_exchange_account(&payer_pubkey, exchange_status);
    program_test.add_account(exchange_pubkey, exchange_account);

    let (mut banks_client, payer, recent_blockhash) = program_test.start().await;
    let recent_blockhash = tokio::sync::RwLock::new(recent_blockhash);

    // Initialize ProgramConfig
    let (program_config_pda, _) = get_program_config_pda(&program_id);
    let ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::InitProgramConfig(InitProgramConfigArgs {}),
        vec![
            AccountMeta::new(program_config_pda, false),
            AccountMeta::new_readonly(program_data_pda, false),
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new_readonly(solana_program::system_program::id(), false),
        ],
    );

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        *recent_blockhash.read().await,
    );
    banks_client.process_transaction(tx).await.unwrap();

    (
        banks_client,
        program_id,
        recent_blockhash,
        payer.pubkey(),
        exchange_pubkey,
    )
}
