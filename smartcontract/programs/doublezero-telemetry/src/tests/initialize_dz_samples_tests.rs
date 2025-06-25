#[cfg(test)]
mod tests {
    use crate::{
        error::TelemetryError, state::dz_latency_samples::DZ_LATENCY_SAMPLES_HEADER_SIZE,
        tests::test_helpers::*,
    };
    use solana_program_test::*;
    use solana_sdk::signature::{Keypair, Signer};

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_success() {
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

        // Verify account creation and data.
        let account = ledger
            .get_account(latency_samples_pda)
            .await
            .unwrap()
            .unwrap();
        assert_eq!(account.owner, ledger.telemetry.program_id);
        assert_eq!(account.data.len(), DZ_LATENCY_SAMPLES_HEADER_SIZE);
        assert_eq!(account.lamports, 2463840);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_unauthorized_agent() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        // Seed ledger with two linked devices, and a funded origin device agent.
        let (_origin_device_agent, origin_device_pk, target_device_pk, link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        // Refresh blockhash to latest before telemetry transaction.
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
