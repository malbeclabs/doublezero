pub mod create_account;
pub mod create_token_account;

use solana_pubkey::Pubkey;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Invoker<'a, 'b> {
    Signer(&'a Pubkey),
    Pda {
        key: &'a Pubkey,
        signer_seeds: &'a [&'b [u8]],
    },
}

impl Invoker<'_, '_> {
    pub fn key(&self) -> &Pubkey {
        match self {
            Invoker::Signer(key) => key,
            Invoker::Pda { key, .. } => key,
        }
    }
}
