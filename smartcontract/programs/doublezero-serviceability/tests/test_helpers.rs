use borsh::to_vec;
use doublezero_serviceability::{
    instructions::*,
    state::{accountdata::AccountData, accounttype::AccountType, globalstate::GlobalState},
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    system_program,
    transaction::Transaction,
};
use std::any::type_name;

#[allow(dead_code)]
pub async fn get_globalstate(
    banks_client: &mut BanksClient,
    globalstate_pubkey: Pubkey,
) -> GlobalState {
    match banks_client.get_account(globalstate_pubkey).await {
        Ok(account) => match account {
            Some(account_data) => {
                let globalstate = GlobalState::from(&account_data.data[..]);
                assert_eq!(globalstate.account_type, AccountType::GlobalState);

                println!("⬅️  Read {globalstate:?}");

                globalstate
            }
            None => panic!("GlobalState account not found"),
        },
        Err(err) => panic!("GlobalState account not found: {err:?}"),
    }
}

#[allow(dead_code)]
pub fn get_type_name<T>() -> String {
    let full_type_name = type_name::<T>();
    if let Some(last_name) = full_type_name.rsplit("::").next() {
        return last_name.to_string();
    }

    "".to_string()
}

#[allow(dead_code)]
pub async fn get_account_data(
    banks_client: &mut BanksClient,
    pubkey: Pubkey,
) -> Option<AccountData> {
    print!("⬅️  Read: ");

    match banks_client.get_account(pubkey).await {
        Ok(account) => match account {
            Some(account_data) => {
                let object = AccountData::from(&account_data.data[..]);
                println!("{object:?}");

                Some(object)
            }
            None => None,
        },
        Err(err) => panic!("account not found: {err:?}"),
    }
}

pub async fn execute_transaction(
    banks_client: &mut BanksClient,
    recent_blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &Keypair,
) {
    print!("➡️  Transaction {instruction:?} ");

    let mut transaction = create_transaction(program_id, instruction, accounts, payer);
    transaction.sign(&[&payer], recent_blockhash);
    banks_client.process_transaction(transaction).await.unwrap();

    println!("✅")
}


#[allow(dead_code)]
pub async fn try_execute_transaction(
    banks_client: &mut BanksClient,
    recent_blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &Keypair,
) -> Result<(), BanksClientError> {
    print!("➡️  Transaction {instruction:?} ");

    let mut transaction = create_transaction(program_id, instruction, accounts, payer);
    transaction.sign(&[&payer], recent_blockhash);
    banks_client.process_transaction(transaction).await?;

    println!("✅");

    Ok(())
}

pub fn create_transaction(
    program_id: Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &Keypair,
) -> Transaction {
    Transaction::new_with_payer(
        &[Instruction::new_with_bytes(
            program_id,
            &to_vec(&instruction).unwrap(),
            [
                accounts,
                vec![
                    AccountMeta::new(payer.pubkey(), true),
                    AccountMeta::new(system_program::id(), false),
                ],
            ]
            .concat(),
        )],
        Some(&payer.pubkey()),
    )
}
