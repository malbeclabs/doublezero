#[cfg(test)]
pub mod test_helpers;

#[cfg(test)]
mod initialize_dz_samples_tests;
#[cfg(test)]
mod write_dz_samples_tests;

#[cfg(test)]
#[ctor::ctor]
fn init_logger() {
    let _ = env_logger::builder()
        // Suppress noisy solana program logs unless the test fails.
        .is_test(true)
        .try_init();
}
