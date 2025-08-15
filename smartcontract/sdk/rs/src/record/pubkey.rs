use solana_sdk::{hash::hashv, pubkey::Pubkey};

/// Create a 32-byte string seed to be used for creating an account with seed.
pub fn create_record_seed_string(seeds: &[&[u8]]) -> String {
    // The full string is 44-bytes.
    let mut seed = hashv(seeds).to_string();

    // Because create-with-seed only supports 32-byte seeds, we need to
    // truncate the above seed. Using this seed is safe because the likelihood
    // of a collision with another seed truncated to 32 bytes is extremely low.
    seed.truncate(32);

    seed
}

/// Create a deterministic record key from a payer key and a slice of seeds. The
/// payer in this case is the authority allowed to write to the record (since it
/// is this account that originally created the record).
pub fn create_record_key(payer_key: &Pubkey, seeds: &[&[u8]]) -> Pubkey {
    let seed_str = create_record_seed_string(seeds);

    // This operation is safe to unwrap because the seed string above is
    // guaranteed to be 32 bytes.
    Pubkey::create_with_seed(&payer_key, &seed_str, &doublezero_record::ID).unwrap()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_create_record_seed_string() {
        let seeds: [&[u8]; 1] = [b"test_create_record_seed_string"];
        let seed_str = create_record_seed_string(&seeds);
        assert_eq!(seed_str.len(), 32);
        assert_eq!(seed_str, "8YGyrUprn2DwKkq3hR2DaqGPYDD5WE1D");
    }

    #[test]
    fn test_create_record_key() {
        let payer_key = Pubkey::new_unique();
        let seeds: [&[u8]; 1] = [b"test_create_record_key"];
        let record_key = create_record_key(&payer_key, &seeds);
        assert_eq!(
            record_key,
            solana_sdk::pubkey!("5ijcs731e8ZwP5UoUai1QM7C1X93DVg4bKsTVzAeNXqn")
        );
    }
}
