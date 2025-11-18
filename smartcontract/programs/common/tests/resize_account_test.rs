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

use doublezero_program_common::resize_account::resize_account_if_needed;

fn test_processor(_program_id: &Pubkey, accounts: &[AccountInfo], ix_data: &[u8]) -> ProgramResult {
    let new_len = u32::from_le_bytes(ix_data.try_into().unwrap()) as usize;
    let mut ai_iter = accounts.iter();
    let payer_ai = next_account_info(&mut ai_iter)?;
    let data_ai = next_account_info(&mut ai_iter)?;
    let system_ai = next_account_info(&mut ai_iter)?;

    let cpi_accounts = [payer_ai.clone(), data_ai.clone(), system_ai.clone()];
    resize_account_if_needed(data_ai, payer_ai, &cpi_accounts, new_len)
}

#[tokio::test]
async fn test_resize_account_grows_account_and_pays_rent() {
    let program_id = Pubkey::new_unique();

    let mut pt = ProgramTest::default();
    pt.prefer_bpf(false);
    pt.add_program(
        "resize_test_program",
        program_id,
        processor!(test_processor),
    );

    let data_pubkey = Pubkey::new_unique();
    let initial_len: usize = 16;
    let initial_lamports: u64 = 1;

    pt.add_account(
        data_pubkey,
        SolanaAccount {
            lamports: initial_lamports,
            data: vec![0u8; initial_len],
            owner: program_id,
            executable: false,
            rent_epoch: 0,
        },
    );

    let (banks_client, payer, recent_blockhash) = pt.start().await;

    let new_len: usize = 200;
    let ix_data = (new_len as u32).to_le_bytes();

    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new(data_pubkey, false),
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

    // Data length updated
    assert_eq!(data_after.data.len(), new_len);

    // Data account lamports brought up to minimum_balance(new_len)
    let rent = Rent::default();
    let required = rent.minimum_balance(new_len);
    assert_eq!(data_after.lamports, required);

    // Amount moved into the data account is exactly the rent delta
    let expected_payment = required - initial_lamports;
    let payment_into_data = data_after.lamports - initial_lamports;
    assert_eq!(payment_into_data, expected_payment);

    // Payer paid at least that much (plus tx fee)
    assert!(
        payer_before.lamports >= payer_after.lamports + expected_payment,
        "payer should lose at least the rent top-up (extra is tx fee)"
    );
}

#[tokio::test]
async fn test_resize_account_shrinks_without_paying_rent() {
    let program_id = Pubkey::new_unique();

    let mut pt = ProgramTest::default();
    pt.prefer_bpf(false);
    pt.add_program(
        "resize_test_program",
        program_id,
        processor!(test_processor),
    );

    let data_pubkey = Pubkey::new_unique();
    let initial_len: usize = 200;
    let rent = Rent::default();
    let initial_lamports: u64 = rent.minimum_balance(initial_len);

    pt.add_account(
        data_pubkey,
        SolanaAccount {
            lamports: initial_lamports,
            data: vec![0u8; initial_len],
            owner: program_id,
            executable: false,
            rent_epoch: 0,
        },
    );

    let (banks_client, payer, recent_blockhash) = pt.start().await;

    let new_len: usize = 50;
    let ix_data = (new_len as u32).to_le_bytes();

    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new(data_pubkey, false),
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

    // 1. Data length shrunk
    assert_eq!(data_after.data.len(), new_len);

    // Data lamports unchanged (no rent payment/refund on shrink)
    assert_eq!(
        data_after.lamports, initial_lamports,
        "shrinking should not change data account lamports"
    );

    // Payer only pays tx fee (so its lamports go down, or equal if fee-free config)
    assert!(
        payer_after.lamports <= payer_before.lamports,
        "payer should not gain lamports as a result of shrink"
    );
}
