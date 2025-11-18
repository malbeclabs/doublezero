use std::convert::TryInto;

use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
    rent::Rent,
    system_program,
};
use solana_program_test::{processor, ProgramTest};
use solana_sdk::{
    account::Account as SolanaAccount,
    instruction::{AccountMeta, Instruction},
    signature::Signer,
    transaction::Transaction,
};

use doublezero_program_common::create_account::try_create_account;

const NEW_ACCOUNT_SEED: &[u8] = b"new-account";

fn test_processor(program_id: &Pubkey, accounts: &[AccountInfo], ix_data: &[u8]) -> ProgramResult {
    let data_len = u32::from_le_bytes(ix_data.try_into().unwrap()) as usize;

    let mut ai_iter = accounts.iter();
    let payer_ai = next_account_info(&mut ai_iter)?;
    let new_ai = next_account_info(&mut ai_iter)?;
    let system_ai = next_account_info(&mut ai_iter)?;

    assert_eq!(*system_ai.key, system_program::id());

    let (_pda, bump) = Pubkey::find_program_address(&[NEW_ACCOUNT_SEED], program_id);
    let bump_seed = [bump];
    let seeds: [&[u8]; 2] = [NEW_ACCOUNT_SEED, &bump_seed];

    try_create_account(
        payer_ai.key,
        new_ai.key,
        new_ai.lamports(),
        data_len,
        program_id,
        accounts,
        &seeds,
    )
}

fn make_program_test(
    program_id: Pubkey,
    new_account_pubkey: Pubkey,
    initial_lamports: u64,
    initial_data_len: usize,
    _new_owner: Pubkey,
) -> ProgramTest {
    let mut pt = ProgramTest::default();
    pt.prefer_bpf(false);
    pt.add_program(
        "try_create_account_test_program",
        program_id,
        processor!(test_processor),
    );

    pt.add_account(
        new_account_pubkey,
        SolanaAccount {
            lamports: initial_lamports,
            data: vec![0u8; initial_data_len],
            owner: system_program::id(),
            executable: false,
            rent_epoch: 0,
        },
    );

    pt
}

#[tokio::test]
async fn test_try_create_account_creates_from_zero_lamports() {
    let program_id = Pubkey::new_unique();
    let (new_account_pubkey, _bump) =
        Pubkey::find_program_address(&[NEW_ACCOUNT_SEED], &program_id);

    let initial_lamports: u64 = 0;
    let initial_data_len: usize = 0;
    let desired_data_len: usize = 128;

    let pt = make_program_test(
        program_id,
        new_account_pubkey,
        initial_lamports,
        initial_data_len,
        program_id,
    );

    let (banks_client, payer, recent_blockhash) = pt.start().await;

    let ix_data = (desired_data_len as u32).to_le_bytes();
    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new(new_account_pubkey, false),
            AccountMeta::new_readonly(system_program::id(), false),
        ],
        data: ix_data.to_vec(),
    };

    let payer_before = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .unwrap();

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        recent_blockhash,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let new_after = banks_client
        .get_account(new_account_pubkey)
        .await
        .unwrap()
        .unwrap();
    let payer_after = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .unwrap();

    let rent = Rent::default();
    let required = rent.minimum_balance(desired_data_len);

    // New account should now exist, owned by the program, with correct size and rent
    assert_eq!(new_after.owner, program_id);
    assert_eq!(new_after.data.len(), desired_data_len);
    assert_eq!(new_after.lamports, required);

    // Payer must have lost at least the rent amount (plus fees)
    let payer_delta = payer_before.lamports - payer_after.lamports;
    assert!(
        payer_delta >= required,
        "payer should pay at least the rent-exemption amount (plus tx fee)"
    );
}

#[tokio::test]
async fn test_try_create_account_tops_up_underfunded_account() {
    let program_id = Pubkey::new_unique();
    let (new_account_pubkey, _bump) =
        Pubkey::find_program_address(&[NEW_ACCOUNT_SEED], &program_id);

    let desired_data_len: usize = 256;
    let rent = Rent::default();
    let required = rent.minimum_balance(desired_data_len);

    let initial_lamports: u64 = required / 2;
    let initial_data_len: usize = 0;

    let pt = make_program_test(
        program_id,
        new_account_pubkey,
        initial_lamports,
        initial_data_len,
        program_id,
    );

    let (banks_client, payer, recent_blockhash) = pt.start().await;

    let ix_data = (desired_data_len as u32).to_le_bytes();
    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new(new_account_pubkey, false),
            AccountMeta::new_readonly(system_program::id(), false),
        ],
        data: ix_data.to_vec(),
    };

    let payer_before = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .unwrap();

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        recent_blockhash,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let new_after = banks_client
        .get_account(new_account_pubkey)
        .await
        .unwrap()
        .unwrap();
    let payer_after = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .unwrap();

    // Allocate + assign + top up to rent-exempt
    assert_eq!(new_after.owner, program_id);
    assert_eq!(new_after.data.len(), desired_data_len);
    assert_eq!(new_after.lamports, required);

    let expected_top_up = required - initial_lamports;
    let actual_top_up = new_after.lamports - initial_lamports;
    assert_eq!(actual_top_up, expected_top_up);

    // Payer must have covered at least that amount (plus fees)
    let payer_delta = payer_before.lamports - payer_after.lamports;
    assert!(
        payer_delta >= expected_top_up,
        "payer should pay at least the lamport_diff (plus tx fee)"
    );
}

#[tokio::test]
async fn test_try_create_account_does_not_top_up_overfunded_account() {
    let program_id = Pubkey::new_unique();
    let (new_account_pubkey, _bump) =
        Pubkey::find_program_address(&[NEW_ACCOUNT_SEED], &program_id);

    let desired_data_len: usize = 64;
    let rent = Rent::default();
    let required = rent.minimum_balance(desired_data_len);

    let initial_lamports: u64 = required + 10;
    let initial_data_len: usize = 0;

    let pt = make_program_test(
        program_id,
        new_account_pubkey,
        initial_lamports,
        initial_data_len,
        program_id,
    );

    let (banks_client, payer, recent_blockhash) = pt.start().await;

    let ix_data = (desired_data_len as u32).to_le_bytes();
    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new(new_account_pubkey, false),
            AccountMeta::new_readonly(system_program::id(), false),
        ],
        data: ix_data.to_vec(),
    };

    let payer_before = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .unwrap();

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        recent_blockhash,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let new_after = banks_client
        .get_account(new_account_pubkey)
        .await
        .unwrap()
        .unwrap();
    let payer_after = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .unwrap();

    // Allocate + assign, but no transfer (lamports unchanged)
    assert_eq!(new_after.owner, program_id);
    assert_eq!(new_after.data.len(), desired_data_len);
    assert_eq!(new_after.lamports, initial_lamports);

    // Payer only pays tx fee (so its lamports go down or stay same if fee-free)
    assert!(
        payer_after.lamports <= payer_before.lamports,
        "payer should not gain lamports as a result of this call"
    );
}
