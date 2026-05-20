//! Acceptance test for issue #3622: the four user-lifecycle instructions
//! that were activator-driven (`ActivateUser`, `RejectUser`, `CloseAccountUser`,
//! `BanUser`) must respond with `DoubleZeroError::Deprecated` (custom code 67).
//! Wire-format discriminants 37/38/43/45 are preserved so old clients still
//! hit a deterministic deprecation error rather than an unknown-instruction
//! decode failure.

use doublezero_serviceability::{
    entrypoint::process_instruction, error::DoubleZeroError, instructions::DoubleZeroInstruction,
};
use solana_program::program_error::ProgramError;
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    signer::Signer,
    transaction::{Transaction, TransactionError},
};

async fn assert_returns_deprecated(instruction: DoubleZeroInstruction) {
    let program_id = Pubkey::new_unique();
    let (banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    let ix = Instruction {
        program_id,
        accounts: vec![AccountMeta::new(payer.pubkey(), true)],
        data: instruction.pack(),
    };
    let mut tx = Transaction::new_with_payer(&[ix], Some(&payer.pubkey()));
    tx.try_sign(&[&payer], recent_blockhash).unwrap();

    let err = banks_client
        .process_transaction(tx)
        .await
        .expect_err("expected deprecated instruction to fail");

    let expected: ProgramError = DoubleZeroError::Deprecated.into();
    let ProgramError::Custom(expected_code) = expected else {
        panic!("Deprecated must map to ProgramError::Custom");
    };

    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(code),
        )) => assert_eq!(
            code, expected_code,
            "expected Deprecated (Custom({expected_code})), got Custom({code})"
        ),
        other => panic!("expected Custom({expected_code}) InstructionError, got {other:?}"),
    }
}

#[tokio::test]
async fn activate_user_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::ActivateUser()).await;
}

#[tokio::test]
async fn reject_user_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::RejectUser()).await;
}

#[tokio::test]
async fn close_account_user_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::CloseAccountUser()).await;
}

#[tokio::test]
async fn ban_user_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::BanUser()).await;
}
