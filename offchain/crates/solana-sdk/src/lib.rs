pub mod passport;
pub mod revenue_distribution;
pub mod shred_subscription;

//

pub use doublezero_program_tools::{
    DISCRIMINATOR_LEN, Discriminator, PrecomputedDiscriminator, get_program_data_address,
    instruction::try_build_instruction, zero_copy,
};
pub use doublezero_revenue_distribution::DOUBLEZERO_MINT_DECIMALS;
pub use doublezero_sol_conversion_interface as sol_conversion;
pub use doublezero_solana_client_tools::rpc::NetworkEnvironment;
pub use solana_sdk::pubkey::Pubkey;
pub use svm_hash::{merkle, sha2};

// TODO: Determine where to remove this duplicate. Re-export?
pub const fn compute_units_for_bump_seed(bump: u8) -> u32 {
    1_500 * (255 - bump) as u32
}

pub fn environment_2z_token_mint_key(network_env: NetworkEnvironment) -> Pubkey {
    match network_env {
        NetworkEnvironment::Testnet | NetworkEnvironment::Devnet => {
            revenue_distribution::env::development::DOUBLEZERO_MINT_KEY
        }
        _ => revenue_distribution::env::mainnet::DOUBLEZERO_MINT_KEY,
    }
}

pub fn environment_usdc_token_mint_key(network_env: NetworkEnvironment) -> Pubkey {
    match network_env {
        NetworkEnvironment::Testnet => shred_subscription::env::development::USDC_MINT_KEY,
        NetworkEnvironment::Devnet => shred_subscription::env::solana_devnet::USDC_MINT_KEY,
        _ => shred_subscription::env::mainnet::USDC_MINT_KEY,
    }
}
