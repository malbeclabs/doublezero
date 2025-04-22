use solana_sdk::pubkey::Pubkey;
use solana_sdk::signature::Keypair;
use std::error::Error;
use std::fs;
use std::str::FromStr;

pub fn read_keypair_from_file(file: String) -> eyre::Result<Keypair, Box<dyn Error>> {
    let file_content = fs::read_to_string(file)?;
    let secret_key_bytes: Vec<u8> = serde_json::from_str(&file_content)?;
    let keypair = Keypair::from_bytes(&secret_key_bytes)?;

    Ok(keypair)
}

pub fn parse_pubkey(input: &str) -> Option<Pubkey> {
    if input.len() < 40 || input.len() > 44 {
        return None;
    }

    match Pubkey::from_str(input) {
        Ok(pk) => Some(pk),
        Err(_) => None,
    }
}
/*
pub fn create_transaction(program_id: Pubkey, instruction: DoubleZeroInstruction, accounts: Vec<AccountMeta>, payer: &Keypair) -> Transaction {

    return Transaction::new_with_payer(
    &[
        Instruction::new_with_bytes(
            program_id,
            &to_vec(&instruction).unwrap(),
            [accounts, vec![
                AccountMeta::new(payer.pubkey(), true),
                AccountMeta::new(system_program::id(), false),
            ]].concat(),
        )
    ], Some(&payer.pubkey()));
}
     */
