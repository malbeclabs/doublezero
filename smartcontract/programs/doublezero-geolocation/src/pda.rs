use solana_program::pubkey::Pubkey;

use crate::seeds::{SEED_PREFIX, SEED_PROGRAM_CONFIG};

pub fn get_program_config_pda(program_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_PROGRAM_CONFIG], program_id)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_program_id() -> Pubkey {
        Pubkey::new_unique()
    }

    #[test]
    fn test_program_config_pda_is_deterministic() {
        let program_id = test_program_id();
        let (pda1, bump1) = get_program_config_pda(&program_id);
        let (pda2, bump2) = get_program_config_pda(&program_id);
        assert_eq!(pda1, pda2);
        assert_eq!(bump1, bump2);
    }

    #[test]
    fn test_program_config_pda_differs_by_program_id() {
        let (pda1, _) = get_program_config_pda(&Pubkey::new_unique());
        let (pda2, _) = get_program_config_pda(&Pubkey::new_unique());
        assert_ne!(pda1, pda2);
    }
}
