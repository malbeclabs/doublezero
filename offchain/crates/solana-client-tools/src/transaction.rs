use anyhow::{Context, Result};
use solana_sdk::{
    hash::Hash,
    instruction::Instruction,
    message::{AddressLookupTableAccount, VersionedMessage, v0::Message},
    signature::Keypair,
    signer::Signer,
    transaction::VersionedTransaction,
};

pub fn new_transaction(
    instructions: &[Instruction],
    signers: &[&Keypair],
    address_lookup_table_accounts: &[AddressLookupTableAccount],
    recent_blockhash: Hash,
) -> Result<VersionedTransaction> {
    let message = Message::try_compile(
        &signers[0].pubkey(),
        instructions,
        address_lookup_table_accounts,
        recent_blockhash,
    )?;

    VersionedTransaction::try_new(VersionedMessage::V0(message), signers)
        .context("Failed to create versioned transaction")
}
