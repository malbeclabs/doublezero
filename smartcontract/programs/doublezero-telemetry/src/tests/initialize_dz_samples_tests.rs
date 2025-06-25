#[cfg(test)]
mod tests {
    use crate::{error::TelemetryError, tests::test_helpers::*};
    use solana_program_test::*;
    use solana_sdk::signature::{Keypair, Signer};

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_success() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        // Seed ledger with two linked devices, and a funded origin device agent.
        let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        // Refresh blockhash to latest blockhash before telemetry transaction.
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

        // Verify account creation and data.
        let account_data_raw = ledger
            .get_account_data(latency_samples_pda)
            .await
            .unwrap()
            .unwrap();
        assert_eq!(account_data_raw.owner, ledger.telemetry.program_id);
        assert_eq!(account_data_raw.data.len(), 400);
        assert_eq!(account_data_raw.lamports, 5247840);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_unauthorized_agent() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        // Seed ledger with two linked devices, and a funded origin device agent.
        let (_origin_device_agent, origin_device_pk, target_device_pk, link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        // Refresh blockhash to latest blockhash before telemetry transaction.
        ledger.refresh_blockhash().await.unwrap();

        // Create and fund an unauthorized agent keypair.
        let unauthorized_agent = Keypair::new();
        let unauthorized_agent_pk = unauthorized_agent.pubkey();
        ledger
            .fund_account(&unauthorized_agent_pk, 10_000_000_000)
            .await
            .unwrap();

        // Execute initialize latency samples transaction with unauthorized agent.
        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(
                &unauthorized_agent,
                origin_device_pk,
                target_device_pk,
                link_pk,
                1u64,
                5_000_000,
            )
            .await;
        assert_telemetry_error(result, TelemetryError::UnauthorizedAgent);
    }
}
