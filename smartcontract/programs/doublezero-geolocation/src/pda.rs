use solana_program::pubkey::Pubkey;

use crate::seeds::{SEED_PREFIX, SEED_PROBE, SEED_PROGRAM_CONFIG};

pub fn get_program_config_pda(program_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_PROGRAM_CONFIG], program_id)
}

pub fn get_geo_probe_pda(program_id: &Pubkey, code: &str) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_PROBE, code.as_bytes()], program_id)
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

    #[test]
    fn test_geo_probe_pda_is_deterministic() {
        let program_id = test_program_id();
        let (pda1, bump1) = get_geo_probe_pda(&program_id, "probe-a");
        let (pda2, bump2) = get_geo_probe_pda(&program_id, "probe-a");
        assert_eq!(pda1, pda2);
        assert_eq!(bump1, bump2);
    }

    #[test]
    fn test_geo_probe_pda_differs_by_code() {
        let program_id = test_program_id();
        let (pda1, _) = get_geo_probe_pda(&program_id, "probe-a");
        let (pda2, _) = get_geo_probe_pda(&program_id, "probe-b");
        assert_ne!(pda1, pda2);
    }

    #[test]
    fn test_geo_probe_pda_differs_by_program_id() {
        let (pda1, _) = get_geo_probe_pda(&Pubkey::new_unique(), "probe-a");
        let (pda2, _) = get_geo_probe_pda(&Pubkey::new_unique(), "probe-a");
        assert_ne!(pda1, pda2);
    }

    #[test]
    fn test_different_pda_types_do_not_collide() {
        let program_id = test_program_id();
        let (config_pda, _) = get_program_config_pda(&program_id);
        let (probe_pda, _) = get_geo_probe_pda(&program_id, "code");
        assert_ne!(config_pda, probe_pda);
    }
}
