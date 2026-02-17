use doublezero_geolocation::instructions::GeolocationInstruction;
use mockall::automock;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[automock]
pub trait GeolocationClient {
    fn get_program_id(&self) -> Pubkey;
    fn execute_transaction(
        &self,
        instruction: GeolocationInstruction,
        accounts: Vec<AccountMeta>,
    ) -> eyre::Result<Signature>;
}
