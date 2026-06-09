pub mod mainnet {
    pub const USDC_MINT_KEY: solana_sdk::pubkey::Pubkey =
        solana_sdk::pubkey!("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v");
}

pub mod development {
    pub const USDC_MINT_KEY: solana_sdk::pubkey::Pubkey =
        solana_sdk::pubkey!("uSDZq2RMuxrEf7gqgDjR8wJCtCyaDAQk2e5jLAaoeeM");
}

pub mod solana_devnet {
    // Circle's USDC mint on Solana devnet, Crossmint uses this one.
    pub const USDC_MINT_KEY: solana_sdk::pubkey::Pubkey =
        solana_sdk::pubkey!("4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU");
}
