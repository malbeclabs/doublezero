use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
    system_program,
};
use solana_program_test::{processor, ProgramTest};
use solana_sdk::{
    account::Account as SolanaAccount,
    instruction::{AccountMeta, Instruction},
    signature::Signer,
    transaction::Transaction,
};

use doublezero_program_common::close_account;

/// Processor:
///   ix_data: unused
/// Accounts:
///   0 - receiving account (gets lamports)
///   1 - account to close (must be owned by program_id)
fn test_processor(
    _program_id: &Pubkey,
    accounts: &[AccountInfo],
    _ix_data: &[u8],
) -> ProgramResult {
    let mut it = accounts.iter();
    let receiving_ai = next_account_info(&mut it)?;
    let close_ai = next_account_info(&mut it)?;

    close_account(close_ai, receiving_ai)
}

fn make_program_test_with_close_account(
    program_id: Pubkey,
    close_pubkey: Pubkey,
    close_lamports: u64,
    close_data_len: usize,
    close_owner: Pubkey,
) -> ProgramTest {
    let mut pt = ProgramTest::default();
    pt.prefer_bpf(false);
    pt.add_program(
        "close_account_test_program",
        program_id,
        processor!(test_processor),
    );

    pt.add_account(
        close_pubkey,
        SolanaAccount {
            lamports: close_lamports,
            data: vec![0u8; close_data_len],
            owner: close_owner,
            executable: false,
            rent_epoch: 0,
        },
    );

    pt
}

#[tokio::test]
async fn test_close_account_transfers_lamports_and_resets_account() {
    let program_id = Pubkey::new_unique();
    let close_pubkey = Pubkey::new_unique();

    let initial_data_len: usize = 32;
    let initial_lamports: u64 = 1_000_000;

    let pt = make_program_test_with_close_account(
        program_id,
        close_pubkey,
        initial_lamports,
        initial_data_len,
        program_id, // owned by the program, so closing is allowed
    );

    let (banks_client, payer, recent_blockhash) = pt.start().await;

    // Use the test harness payer as the receiving account.
    let receiver_pubkey = payer.pubkey();

    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(receiver_pubkey, false), // receiver, writable
            AccountMeta::new(close_pubkey, false),    // account to close
        ],
        data: vec![], // no data needed
    };

    let receiver_before = banks_client
        .get_account(receiver_pubkey)
        .await
        .unwrap()
        .unwrap();

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&receiver_pubkey),
        &[&payer],
        recent_blockhash,
    );
    banks_client.process_transaction(tx).await.unwrap();

    let receiver_after = banks_client
        .get_account(receiver_pubkey)
        .await
        .unwrap()
        .unwrap();

    // Closed account may be fully removed (None) or exist as a 0/0/system account.
    let close_after_opt = banks_client.get_account(close_pubkey).await.unwrap();

    // Receiver should have gained *some* lamports from the closed account,
    // but because it's also the payer, tx fees reduce the net gain.
    let receiver_gain = receiver_after.lamports - receiver_before.lamports;
    assert!(
        receiver_gain > 0 && receiver_gain <= initial_lamports,
        "receiver should gain > 0 and at most the closed account's lamports (fee is paid from receiver)"
    );

    if let Some(close_after) = close_after_opt {
        // If the account is still present, it should be reset.
        assert_eq!(close_after.lamports, 0);
        assert_eq!(close_after.data.len(), 0);
        assert_eq!(close_after.owner, system_program::id());
    } else {
        // Also acceptable: runtime cleaned up a zero-lamport, zero-data system account.
        // This still means "closed" from a program logic perspective.
    }
}

#[tokio::test]
async fn test_close_account_fails_if_not_owned_by_program() {
    let program_id = Pubkey::new_unique();
    let close_pubkey = Pubkey::new_unique();

    let initial_data_len: usize = 16;
    let initial_lamports: u64 = 500_000;

    // Make the account owned by system_program, not by our program_id.
    let pt = make_program_test_with_close_account(
        program_id,
        close_pubkey,
        initial_lamports,
        initial_data_len,
        system_program::id(),
    );

    let (banks_client, payer, recent_blockhash) = pt.start().await;

    let receiver_pubkey = payer.pubkey();

    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(receiver_pubkey, false),
            AccountMeta::new(close_pubkey, false),
        ],
        data: vec![],
    };

    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&receiver_pubkey),
        &[&payer],
        recent_blockhash,
    );

    // Expect an error due to IncorrectProgramId in close_account
    let res = banks_client.process_transaction(tx).await;
    assert!(
        res.is_err(),
        "closing an account not owned by the program should error"
    );

    // Verify that lamports on the would-be closed account didn't change
    let close_after = banks_client
        .get_account(close_pubkey)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(close_after.lamports, initial_lamports);
    assert_eq!(close_after.data.len(), initial_data_len);
    assert_eq!(close_after.owner, system_program::id());
}
