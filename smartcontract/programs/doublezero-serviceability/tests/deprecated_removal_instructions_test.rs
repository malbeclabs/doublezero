//! Issue #2470: the general-purpose removal instructions are replaced by one per pass type.
//! Wire discriminants 69 and 42 are kept so an old client hits a deterministic deprecation
//! error instead of an unknown-instruction decode failure.

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
async fn close_access_pass_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::CloseAccessPass()).await;
}

#[tokio::test]
async fn delete_user_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::DeleteUser()).await;
}
