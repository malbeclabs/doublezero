use crate::{
    entrypoint::process_instruction as telemetry_process_instruction,
    instructions::TelemetryInstruction,
};
use solana_program_test::*;
use solana_sdk::{
    account::Account,
    commitment_config::CommitmentLevel,
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    system_program,
    transaction::Transaction,
};

pub async fn get_account_data(banks_client: &mut BanksClient, pubkey: Pubkey) -> Option<Account> {
    banks_client.get_account(pubkey).await.unwrap()
}

pub async fn fund_account(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    recipient: &Pubkey,
    lamports: u64,
    recent_blockhash: solana_sdk::hash::Hash,
) -> Result<(), BanksClientError> {
    let transfer_instruction =
        solana_sdk::system_instruction::transfer(&payer.pubkey(), recipient, lamports);
    let mut transaction =
        Transaction::new_with_payer(&[transfer_instruction], Some(&payer.pubkey()));
    transaction.sign(&[payer], recent_blockhash);
    banks_client.process_transaction(transaction).await
}

// Execute telemetry transaction with specific signers
pub async fn execute_transaction(
    banks_client: &mut BanksClient,
    signers: &[&Keypair],
    recent_blockhash: solana_sdk::hash::Hash,
    program_id: Pubkey,
    instruction: TelemetryInstruction,
    accounts: Vec<AccountMeta>,
) -> Result<(), BanksClientError> {
    let instruction_data = instruction
        .pack()
        .map_err(|_| BanksClientError::ClientError("Failed to pack instruction"))?;

    let payer = signers[0]; // First signer is always the payer
    let mut transaction = Transaction::new_with_payer(
        &[Instruction {
            program_id,
            accounts,
            data: instruction_data,
        }],
        Some(&payer.pubkey()),
    );
    transaction.sign(signers, recent_blockhash);
    banks_client
        .process_transaction_with_commitment(transaction, CommitmentLevel::Processed)
        .await
        .map_err(|e| {
            println!("Transaction failed: {:?}", e);
            e
        })?;
    Ok(())
}

// Helper to execute serviceability instructions for setting up test data
pub async fn execute_serviceability_instruction(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    recent_blockhash: solana_sdk::hash::Hash,
    program_id: Pubkey,
    instruction: doublezero_serviceability::instructions::DoubleZeroInstruction,
    mut accounts: Vec<AccountMeta>,
) -> Result<(), BanksClientError> {
    // Automatically append payer and system_program
    accounts.push(AccountMeta::new(payer.pubkey(), true));
    accounts.push(AccountMeta::new_readonly(system_program::id(), false));

    let instruction_data = borsh::to_vec(&instruction).unwrap();

    let mut transaction = Transaction::new_with_payer(
        &[Instruction {
            program_id,
            accounts,
            data: instruction_data,
        }],
        Some(&payer.pubkey()),
    );
    transaction.sign(&[payer], recent_blockhash);
    banks_client.process_transaction(transaction).await
}

pub fn setup_test_programs() -> (ProgramTest, Pubkey, Pubkey) {
    let mut program_test = ProgramTest::default();
    program_test.set_compute_max_units(1_000_000);

    // Add telemetry program
    let telemetry_program_id = Pubkey::new_unique();
    program_test.add_program(
        "doublezero_telemetry",
        telemetry_program_id,
        processor!(telemetry_process_instruction),
    );

    // Add serviceability program with its actual processor
    let serviceability_program_id = Pubkey::new_unique();
    program_test.add_program(
        "doublezero_serviceability",
        serviceability_program_id,
        processor!(doublezero_serviceability::test_support::process_instruction_for_tests),
    );

    (
        program_test,
        telemetry_program_id,
        serviceability_program_id,
    )
}
