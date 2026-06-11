//! Acceptance test for issue #3623: the 13 contributor-side program
//! instructions that were activator-driven must respond with
//! `DoubleZeroError::Deprecated` (custom code 67). Wire-format discriminants
//! 21/22/27/29/30/35/47/48/53/72/75/77/78 are preserved so old clients still
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
async fn activate_device_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::ActivateDevice()).await;
}

#[tokio::test]
async fn reject_device_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::RejectDevice()).await;
}

#[tokio::test]
async fn close_account_device_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::CloseAccountDevice()).await;
}

#[tokio::test]
async fn activate_link_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::ActivateLink()).await;
}

#[tokio::test]
async fn reject_link_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::RejectLink()).await;
}

#[tokio::test]
async fn close_account_link_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::CloseAccountLink()).await;
}

#[tokio::test]
async fn activate_multicastgroup_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::ActivateMulticastGroup()).await;
}

#[tokio::test]
async fn reject_multicastgroup_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::RejectMulticastGroup()).await;
}

#[tokio::test]
async fn deactivate_multicastgroup_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::DeactivateMulticastGroup()).await;
}

#[tokio::test]
async fn activate_device_interface_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::ActivateDeviceInterface()).await;
}

#[tokio::test]
async fn remove_device_interface_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::RemoveDeviceInterface()).await;
}

#[tokio::test]
async fn unlink_device_interface_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::UnlinkDeviceInterface()).await;
}

#[tokio::test]
async fn reject_device_interface_returns_deprecated() {
    assert_returns_deprecated(DoubleZeroInstruction::RejectDeviceInterface()).await;
}
