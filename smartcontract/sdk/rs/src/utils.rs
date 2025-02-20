use std::fs;
use std::error::Error;
use solana_sdk::signature::Keypair;


pub fn read_keypair_from_file(file: String) -> eyre::Result<Keypair, Box<dyn Error>> {

    let file_content = fs::read_to_string(file)?;
    let secret_key_bytes: Vec<u8> = serde_json::from_str(&file_content)?;
    let keypair = Keypair::from_bytes(&secret_key_bytes)?;

    Ok(keypair)
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