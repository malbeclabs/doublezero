#[cfg(test)]
mod tests {
    use crate::{
        constants::DZ_LATENCY_SAMPLES_MAX_SIZE, state::dz_latency_samples::DzLatencySamples,
        tests::test_helpers::*,
    };
    use solana_program_test::*;

    #[tokio::test]
    async fn test_write_dz_latency_samples_success() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        // Seed ledger with two linked devices, and a funded origin device agent.
        let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        // Refresh blockhash to latest before telemetry transaction.
        ledger.refresh_blockhash().await.unwrap();

        // Execute initialize latency samples transaction.
        let latency_samples_pda = ledger
            .telemetry
            .initialize_dz_latency_samples(
                &origin_device_agent,
                origin_device_pk,
                target_device_pk,
                link_pk,
                1u64,
                5_000_000,
            )
            .await
            .unwrap();

        let account = ledger
            .get_account(latency_samples_pda)
            .await
            .unwrap()
            .expect("Latency samples account does not exist");
        assert_eq!(account.owner, ledger.telemetry.program_id);
        assert_eq!(account.data.len(), DZ_LATENCY_SAMPLES_MAX_SIZE);

        let samples_data = DzLatencySamples::try_from(&account.data[..]).unwrap();
        assert_eq!(samples_data.start_timestamp_microseconds, 0);
        assert_eq!(samples_data.next_sample_index, 0);
        assert_eq!(samples_data.samples, Vec::<u32>::new());

        // Write samples to account.
        let samples_to_write = vec![1000, 1200, 1100];
        let current_timestamp = 1_700_000_000_000_100; // Example timestamp
        ledger
            .telemetry
            .write_dz_latency_samples(
                &origin_device_agent,
                latency_samples_pda,
                samples_to_write.clone(),
                current_timestamp,
            )
            .await
            .unwrap();

        // Verify samples were written.
        let account = ledger
            .get_account(latency_samples_pda)
            .await
            .unwrap()
            .expect("Latency samples account does not exist");

        let samples_data = DzLatencySamples::try_from(&account.data[..]).unwrap();
        assert_eq!(samples_data.start_timestamp_microseconds, current_timestamp);
        assert_eq!(
            samples_data.next_sample_index,
            samples_to_write.len() as u32
        );
        assert_eq!(samples_data.samples, samples_to_write);

        // Write more samples.
        let more_samples = vec![1300, 1400];
        let new_timestamp = 1_700_000_000_000_200; // Later timestamp, should not overwrite original start
        ledger
            .telemetry
            .write_dz_latency_samples(
                &origin_device_agent,
                latency_samples_pda,
                more_samples.clone(),
                new_timestamp,
            )
            .await
            .unwrap();

        // Verify samples were written.
        let account = ledger
            .get_account(latency_samples_pda)
            .await
            .unwrap()
            .expect("Latency samples account does not exist");

        let samples_data = DzLatencySamples::try_from(&account.data[..]).unwrap();
        assert_eq!(samples_data.start_timestamp_microseconds, current_timestamp);
        assert_eq!(
            samples_data.next_sample_index,
            samples_to_write.len() as u32 + more_samples.len() as u32
        );
    }

    // Add more tests for WriteDzLatencySamples failure cases:
    // - test_write_dz_latency_samples_fail_account_not_exist()
    // - test_write_dz_latency_samples_fail_unauthorized_agent()
    // - test_write_dz_latency_samples_fail_epoch_mismatch()
    // - test_write_dz_latency_samples_fail_account_full()
    // - test_write_dz_latency_samples_fail_invalid_pda_components()
}
