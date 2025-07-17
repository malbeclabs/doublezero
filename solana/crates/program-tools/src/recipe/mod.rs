pub mod create_account;

use solana_pubkey::Pubkey;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Invoker<'a, 'b> {
    Signer(&'a Pubkey),
    Pda {
        key: &'a Pubkey,
        signer_seeds: &'a [&'b [u8]],
    },
}
