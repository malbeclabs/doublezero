use sha2::{Digest, Sha256};
use solana_program::pubkey::{Pubkey, PubkeyError};

pub const SEED_PREFIX: &[u8] = b"telemetry";
pub const SEED_DEVICE_LATENCY_SAMPLES: &[u8] = b"device-latency-samples";

/// Computes a 32-character base58-encoded seed for use with `create_with_seed`
/// when deriving the latency samples account address.
pub fn derive_device_latency_samples_account_seed(
    program_id: &Pubkey,
    origin_device_pk: &Pubkey,
    target_device_pk: &Pubkey,
    link_pk: &Pubkey,
    epoch: u64,
) -> String {
    let mut hasher = Sha256::new();
    hasher.update(program_id.as_ref());
    hasher.update(SEED_PREFIX);
    hasher.update(SEED_DEVICE_LATENCY_SAMPLES);
    hasher.update(origin_device_pk.as_ref());
    hasher.update(target_device_pk.as_ref());
    hasher.update(link_pk.as_ref());
    hasher.update(&epoch.to_le_bytes());

    let hash = hasher.finalize();
    let seed = &bs58::encode(&hash[..]).into_string()[..32];

    seed.to_string()
}

/// Derives a deterministic account address for storing device latency samples.
///
/// The address is derived using `Pubkey::create_with_seed`, using a base agent pubkey
/// and a hashed seed derived from the program ID, origin/target device keys, link key, and epoch.
///
/// Note: This is not a true Solana Program Derived Address (PDA) and does not guarantee
/// that the address lies off the ed25519 curve. It is a seeded address that must be
/// created and owned by the program.
pub fn derive_device_latency_samples_account(
    agent: &Pubkey,
    program_id: &Pubkey,
    origin_device_pk: &Pubkey,
    target_device_pk: &Pubkey,
    link_pk: &Pubkey,
    epoch: u64,
) -> Result<Pubkey, PubkeyError> {
    let seed = derive_device_latency_samples_account_seed(
        program_id,
        origin_device_pk,
        target_device_pk,
        link_pk,
        epoch,
    );

    Pubkey::create_with_seed(agent, &seed, program_id)
}

#[cfg(test)]
mod tests {
    use std::str::FromStr;

    use super::*;

    #[test]
    fn test_derive_device_latency_samples_account() {
        let program_id = Pubkey::from_str("8x3X1VRUUqZ2UDs2xMo5V2d2Kk2yCNTe2TG7PsfE2uDw").unwrap();
        let agent = Pubkey::from_str("2QtuSEANvdN9x6uFJZKdA45ZKq8YFvo1Ho4DP47vRLHP").unwrap();
        let origin = Pubkey::from_str("DEvCe1kUN9TkfbHyi9RQ2cn8s4cDQFroC1Z9bbz6Hnm9").unwrap();
        let target = Pubkey::from_str("DEvCe2q4C64rDfK6bZc6JYvXcysQHoekWEiZ1vT14uxW").unwrap();
        let link = Pubkey::from_str("5EYw8N3K4nU6utduZu3d6C8yV2QQGHkEgr6n8XztRLMN").unwrap();
        let epoch = 12345;

        let seed =
            derive_device_latency_samples_account_seed(&program_id, &origin, &target, &link, epoch);
        assert_eq!(seed.len(), 32);

        let addr = derive_device_latency_samples_account(
            &agent,
            &program_id,
            &origin,
            &target,
            &link,
            epoch,
        )
        .unwrap();

        assert_eq!(seed, "9QgHDpkkJTDnFP1kSt5SmN3AnwZBQ4xo");
        assert_eq!(
            addr.to_string(),
            "AS8o3BVc9cptTcgV2ihBNfzwK3mYZbstnnB7gACcYL1e"
        );
    }
}
