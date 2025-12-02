use bytemuck::{Pod, Zeroable};
use doublezero_program_tools::{types::StorageGap, Discriminator, PrecomputedDiscriminator};
use solana_pubkey::Pubkey;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Pod, Zeroable)]
#[repr(C, align(8))]
pub struct SolanaValidatorDeposit {
    pub node_id: Pubkey,

    pub written_off_sol_debt: u64,
    _padding: [u8; 24],

    _storage_gap: StorageGap<1>,
}

impl PrecomputedDiscriminator for SolanaValidatorDeposit {
    const DISCRIMINATOR: Discriminator<8> =
        Discriminator::new_sha2(b"dz::account::solana_validator_deposit");
}

impl SolanaValidatorDeposit {
    pub const SEED_PREFIX: &'static [u8] = b"solana_validator_deposit";

    pub fn find_address(node_id: &Pubkey) -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX, node_id.as_ref()], &crate::ID)
    }
}

//

const _: () = assert!(
    size_of::<SolanaValidatorDeposit>() == 96,
    "`SolanaValidatorDeposit` size changed"
);
