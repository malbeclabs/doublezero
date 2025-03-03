use double_zero_sla_program::instructions::DoubleZeroInstruction;
use solana_sdk::{instruction::AccountMeta, signature::Signature};


pub struct DZTransaction {
    pub instruction: DoubleZeroInstruction,
    pub accounts: Vec<AccountMeta>,
    pub log_messages: Vec<String>,
    pub signature: Signature,
}
