use doublezero_solana_client_tools::payer::Wallet;
use solana_program_test::ProgramTest;
use solana_sdk::{signature::Signer, transaction::Transaction};

// Calibration guard for Wallet::memo_compute_units. Runs the real spl-memo v3
// program (bundled by solana-program-test) across a range of byte lengths and
// confirms the estimate stays a safe ceiling over actual consumption without
// grossly over provisioning. A toolchain bump that moves the program cost trips
// this test.
#[tokio::test]
async fn memo_compute_units_covers_actual_consumption() {
    let (banks_client, payer, recent_blockhash) = ProgramTest::default().start().await;

    // Spans the lengths callers use ("Relay" is 5 bytes, the validator deposit
    // memos run to about 32) plus larger samples to confirm the line holds.
    let lengths = [0, 5, 6, 24, 32, 64, 128, 256];
    for len in lengths {
        let memo = vec![b'a'; len];
        let (memo_ix, memo_cu) = Wallet::build_memo_instruction_with_compute_units(&memo);
        let transaction = Transaction::new_signed_with_payer(
            &[memo_ix],
            Some(&payer.pubkey()),
            &[&payer],
            recent_blockhash,
        );
        let outcome = banks_client
            .process_transaction_with_metadata(transaction)
            .await
            .unwrap();
        assert!(outcome.result.is_ok(), "len {len}: {:?}", outcome.result);

        let consumed = outcome.metadata.unwrap().compute_units_consumed;
        let memo_cu = u64::from(memo_cu);
        assert!(
            memo_cu >= consumed,
            "len {len}: estimate {memo_cu} below consumed {consumed}"
        );
        // Guard against regressing to a wasteful flat over-estimate.
        assert!(
            memo_cu <= consumed * 2,
            "len {len}: estimate {memo_cu} more than double consumed {consumed}"
        );
    }
}
