pub mod instruction;
#[cfg(feature = "entrypoint")]
mod processor;
pub mod state;

//

solana_pubkey::declare_id!("mriRBpCwjydsE5ZZpXNnXnELcUhsHZTf1FqEHvUkFWe");

/// The address the mock expects in a monthly withdraw's program config slot. No account is created
/// there, because the mock only checks that rev-distr forwarded the key the caller passed.
pub fn find_program_config_address() -> (solana_pubkey::Pubkey, u8) {
    solana_pubkey::Pubkey::find_program_address(&[b"mock_program_config"], &ID)
}
