use solana_program::pubkey::Pubkey;

use crate::seeds::{SEED_GEOUSER, SEED_PREFIX, SEED_PROBE, SEED_PROGRAM_CONFIG};

pub fn get_program_config_pda(program_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_PROGRAM_CONFIG], program_id)
}

pub fn get_geo_probe_pda(program_id: &Pubkey, code: &str) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_PROBE, code.as_bytes()], program_id)
}

pub fn get_geolocation_user_pda(program_id: &Pubkey, code: &str) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_GEOUSER, code.as_bytes()], program_id)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_geo_probe_pda_differs_by_code() {
        let program_id = Pubkey::new_unique();
        let (pda1, _) = get_geo_probe_pda(&program_id, "probe-a");
        let (pda2, _) = get_geo_probe_pda(&program_id, "probe-b");
        assert_ne!(pda1, pda2);
    }

    #[test]
    fn test_geolocation_user_pda_differs_by_code() {
        let program_id = Pubkey::new_unique();
        let (pda1, _) = get_geolocation_user_pda(&program_id, "user-a");
        let (pda2, _) = get_geolocation_user_pda(&program_id, "user-b");
        assert_ne!(pda1, pda2);
    }
}
