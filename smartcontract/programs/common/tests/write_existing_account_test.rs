use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
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

use doublezero_program_common::write_existing_account;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Eq, Clone)]
struct MyAccount {
    index: u64,
    value: u64,
}

/// Instruction:
///   ix_data = Borsh-serialized MyAccount
/// Accounts:
///   0 - payer (signer)
///   1 - account to update (must be owned by program_id)
///   2 - system_program
fn test_processor(_program_id: &Pubkey, accounts: &[AccountInfo], ix_data: &[u8]) -> ProgramResult {
    let instance: MyAccount = BorshDeserialize::try_from_slice(ix_data)
        .map_err(|_| ProgramError::InvalidInstructionData)?;

    let mut ai_iter = accounts.iter();
    let payer_ai = next_account_info(&mut ai_iter)?;
    let data_ai = next_account_info(&mut ai_iter)?;
    let system_ai = next_account_info(&mut ai_iter)?;

    write_existing_account(data_ai, payer_ai, system_ai, &instance)
}

fn make_program_test(
    program_id: Pubkey,
    data_pubkey: Pubkey,
    lamports: u64,
    data: Vec<u8>,
    owner: Pubkey,
) -> ProgramTest {
    let mut pt = ProgramTest::default();
    pt.prefer_bpf(false);
    pt.add_program(
        "write_existing_account_test_program",
        program_id,
        processor!(test_processor),
    );

    pt.add_account(
        data_pubkey,
        SolanaAccount {
            lamports,
            data,
            owner,
            executable: false,
            rent_epoch: 0,
        },
    );

    pt
}

#[tokio::test]
async fn test_write_existing_account_overwrites_without_resizing() {
    let program_id = Pubkey::new_unique();
    let data_pubkey = Pubkey::new_unique();

    // Initial instance already stored in the account, rent-exempt and correct size.
    let initial_instance = MyAccount {
        index: 1,
        value: 100,
    };
    let initial_bytes = borsh::to_vec(&initial_instance).unwrap();
    let initial_len = initial_bytes.len();

    let rent = Rent::default();
    let initial_lamports = rent.minimum_balance(initial_len);

    let pt = make_program_test(
        program_id,
        data_pubkey,
        initial_lamports,
        initial_bytes.clone(),
        program_id,
    );
    let (banks_client, payer, recent_blockhash) = pt.start().await;

    // New instance with same type/size but different value.
    let updated_instance = MyAccount {
        index: 1,
        value: 9999,
    };
    let updated_bytes = borsh::to_vec(&updated_instance).unwrap();
    let updated_len = updated_bytes.len();
    assert_eq!(
        initial_len, updated_len,
        "sizes should match for non-resize path"
    );

    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new(data_pubkey, false),
            AccountMeta::new_readonly(system_program::id(), false),
        ],
        data: updated_bytes.clone(),
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

    let data_after = banks_client
        .get_account(data_pubkey)
        .await
        .unwrap()
        .unwrap();
    let payer_after = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .unwrap();

    // Length unchanged, lamports unchanged (no resize / no extra rent).
    assert_eq!(data_after.data.len(), initial_len);
    assert_eq!(
        data_after.lamports, initial_lamports,
        "lamports should not change when no resize is needed"
    );

    // Data should now be the updated instance.
    let loaded: MyAccount = BorshDeserialize::try_from_slice(&data_after.data).unwrap();
    assert_eq!(loaded, updated_instance);

    // Payer may have paid only tx fee (or nothing, depending on config), but must not gain lamports.
    assert!(
        payer_after.lamports <= payer_before.lamports,
        "payer should not gain lamports from write_existing_account"
    );
}

#[tokio::test]
async fn test_write_existing_account_resizes_and_tops_up_rent() {
    let program_id = Pubkey::new_unique();
    let data_pubkey = Pubkey::new_unique();

    // Target instance we want to store.
    let instance = MyAccount {
        index: 7,
        value: 123_456,
    };
    let serialized = borsh::to_vec(&instance).unwrap();
    let new_len = serialized.len();

    let rent = Rent::default();
    let required_lamports = rent.minimum_balance(new_len);

    // Start with an account that:
    // - is program-owned
    // - has a smaller data len
    // - is underfunded for the required size
    let initial_len: usize = new_len / 2; // smaller than needed
    let initial_lamports: u64 = required_lamports / 2; // underfunded

    let pt = make_program_test(
        program_id,
        data_pubkey,
        initial_lamports,
        vec![0u8; initial_len],
        program_id,
    );
    let (banks_client, payer, recent_blockhash) = pt.start().await;

    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new(data_pubkey, false),
            AccountMeta::new_readonly(system_program::id(), false),
        ],
        data: serialized.clone(),
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

    let data_after = banks_client
        .get_account(data_pubkey)
        .await
        .unwrap()
        .unwrap();
    let payer_after = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .unwrap();

    // Account should have been resized to new_len and topped up to rent-exempt.
    assert_eq!(data_after.data.len(), new_len);
    assert_eq!(
        data_after.lamports, required_lamports,
        "account should be topped up to rent-exempt for new_len"
    );

    // Data must match the instance we wrote.
    let loaded: MyAccount = BorshDeserialize::try_from_slice(&data_after.data).unwrap();
    assert_eq!(loaded, instance);

    let expected_top_up = required_lamports - initial_lamports;
    let actual_top_up = data_after.lamports - initial_lamports;
    assert_eq!(
        actual_top_up, expected_top_up,
        "lamports added to account should match rent top-up"
    );

    let payer_delta = payer_before.lamports - payer_after.lamports;
    assert!(
        payer_delta >= expected_top_up,
        "payer should pay at least the rent top-up (plus tx fee)"
    );
}
