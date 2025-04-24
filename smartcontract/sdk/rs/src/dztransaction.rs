use chrono::{DateTime, Utc};
use doublezero_sla_program::instructions::DoubleZeroInstruction;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, Clone)]
pub struct DZTransaction {
    pub time: DateTime<Utc>,
    pub account: Pubkey,
    pub instruction: DoubleZeroInstruction,
    pub log_messages: Vec<String>,
    pub signature: Signature,
}
