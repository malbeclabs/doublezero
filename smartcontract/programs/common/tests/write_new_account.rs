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

use doublezero_program_common::write_new_account;

const SEED_PREFIX: &[u8] = b"test-account";

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Eq, Clone)]
struct MyAccount {
    index: u64,
    value: u64,
}

fn derive_pda(instance: &MyAccount, program_id: &Pubkey) -> (Pubkey, u8) {
    let seeds = [SEED_PREFIX, &instance.index.to_le_bytes()];
    Pubkey::find_program_address(&seeds, program_id)
}

/// ix_data = Borsh-serialized MyAccount
/// Accounts:
///   0 - payer (signer)
///   1 - PDA account
///   2 - system_program
fn test_processor(program_id: &Pubkey, accounts: &[AccountInfo], ix_data: &[u8]) -> ProgramResult {
    let instance: MyAccount = BorshDeserialize::try_from_slice(ix_data)
        .map_err(|_| ProgramError::InvalidInstructionData)?;

    let mut ai_iter = accounts.iter();
    let payer_ai = next_account_info(&mut ai_iter)?;
    let pda_ai = next_account_info(&mut ai_iter)?;
    let system_ai = next_account_info(&mut ai_iter)?;

    let (expected_pda, bump) = derive_pda(&instance, program_id);
    if *pda_ai.key != expected_pda {
        return Err(ProgramError::InvalidSeeds);
    }

    let bump_bytes = [bump];
    let pda_seeds: [&[u8]; 3] = [SEED_PREFIX, &instance.index.to_le_bytes(), &bump_bytes];

    write_new_account(
        pda_ai, payer_ai, system_ai, program_id, &instance, &pda_seeds,
    )
}

fn make_program_test(
    program_id: Pubkey,
    pda: Pubkey,
    initial_lamports: u64,
    initial_data_len: usize,
) -> ProgramTest {
    let mut pt = ProgramTest::default();
    pt.prefer_bpf(false);
    pt.add_program(
        "write_new_account_test_program",
        program_id,
        processor!(test_processor),
    );

    pt.add_account(
        pda,
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
async fn test_write_new_account_creates_from_zero_lamports() {
    let program_id = Pubkey::new_unique();

    let instance = MyAccount {
        index: 1,
        value: 42,
    };
    let (pda, _bump) = derive_pda(&instance, &program_id);

    let initial_lamports: u64 = 0;
    let initial_data_len: usize = 0;

    let pt = make_program_test(program_id, pda, initial_lamports, initial_data_len);
    let (banks_client, payer, recent_blockhash) = pt.start().await;

    let ix_data = borsh::to_vec(&instance).unwrap();
    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new(pda, false),
            AccountMeta::new_readonly(system_program::id(), false),
        ],
        data: ix_data,
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

    let pda_after = banks_client.get_account(pda).await.unwrap().unwrap();
    let payer_after = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .unwrap();

    let serialized_len = borsh::to_vec(&instance).unwrap().len();
    let rent = Rent::default();
    let required_lamports = rent.minimum_balance(serialized_len);

    // PDA should now be owned by the program, with correct data len and rent
    assert_eq!(pda_after.owner, program_id);
    assert_eq!(pda_after.data.len(), serialized_len);
    assert_eq!(pda_after.lamports, required_lamports);

    // Data round-trips via Borsh
    let loaded: MyAccount = BorshDeserialize::try_from_slice(&pda_after.data).unwrap();
    assert_eq!(loaded, instance);

    // Payer must have paid at least the rent amount (plus tx fee)
    let payer_delta = payer_before.lamports - payer_after.lamports;
    assert!(
        payer_delta >= required_lamports,
        "payer should pay at least the rent-exemption amount (plus tx fee)"
    );
}

#[tokio::test]
async fn test_write_new_account_fixes_underfunded_existing_account() {
    let program_id = Pubkey::new_unique();

    let instance = MyAccount {
        index: 7,
        value: 999_999,
    };
    let (pda, _bump) = derive_pda(&instance, &program_id);

    let serialized_len = borsh::to_vec(&instance).unwrap().len();
    let rent = Rent::default();
    let required_lamports = rent.minimum_balance(serialized_len);

    // Start with some lamports, but not enough to be rent-exempt, and no data.
    // This matches the "pre-funded but unallocated" case try_create_account is designed for.
    let initial_lamports: u64 = required_lamports / 2;
    let initial_data_len: usize = 0;

    let pt = make_program_test(program_id, pda, initial_lamports, initial_data_len);
    let (banks_client, payer, recent_blockhash) = pt.start().await;

    let ix_data = borsh::to_vec(&instance).unwrap();
    let ix = Instruction {
        program_id,
        accounts: vec![
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new(pda, false),
            AccountMeta::new_readonly(system_program::id(), false),
        ],
        data: ix_data,
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

    let pda_after = banks_client.get_account(pda).await.unwrap().unwrap();
    let payer_after = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .unwrap();

    // PDA should now be owned by program, fixed size, and fully rent-exempt
    assert_eq!(pda_after.owner, program_id);
    assert_eq!(pda_after.data.len(), serialized_len);
    assert_eq!(pda_after.lamports, required_lamports);

    // Data round-trips
    let loaded: MyAccount = BorshDeserialize::try_from_slice(&pda_after.data).unwrap();
    assert_eq!(loaded, instance);

    // Account was topped up by exactly the difference
    let expected_top_up = required_lamports - initial_lamports;
    let actual_top_up = pda_after.lamports - initial_lamports;
    assert_eq!(actual_top_up, expected_top_up);

    // Payer covered at least that much (plus transaction fee)
    let payer_delta = payer_before.lamports - payer_after.lamports;
    assert!(
        payer_delta >= expected_top_up,
        "payer should pay at least the lamport_diff (plus tx fee)"
    );
}
