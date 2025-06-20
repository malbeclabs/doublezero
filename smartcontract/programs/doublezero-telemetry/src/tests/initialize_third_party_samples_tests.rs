#[cfg(test)]
mod tests {
    use crate::{
        constants::{MAX_SAMPLES, THIRDPARTY_LATENCY_SAMPLES_MAX_SIZE},
        instructions::TelemetryInstruction,
        pda::derive_thirdparty_latency_samples_pda,
        processors::telemetry::initialize_thirdparty_samples::InitializeThirdPartyLatencySamplesArgs,
        state::{accounttype::AccountType, thirdparty_latency_samples::ThirdPartyLatencySamples},
        tests::test_helpers::*,
    };
    use borsh::BorshDeserialize;
    use doublezero_serviceability::state::location::{Location, LocationStatus};
    use solana_program_test::*;
    use solana_sdk::{
        instruction::AccountMeta,
        pubkey::Pubkey,
        signature::{Keypair, Signer},
        system_program,
    };

    #[tokio::test]
    async fn test_initialize_thirdparty_latency_samples_success() {
        let mut program_test = ProgramTest::default();
        let (telemetry_program_id, serviceability_program_id) =
            add_programs_for_telemetry_tests(&mut program_test);

        let agent_keypair = Keypair::new();
        let location_a_pk = Pubkey::new_unique();
        let location_z_pk = Pubkey::new_unique();
        let epoch = 1u64;
        let mut data_provider_name_bytes = [0u8; 32];
        let provider_name_str = "TestProvider";
        data_provider_name_bytes[..provider_name_str.len()]
            .copy_from_slice(provider_name_str.as_bytes());

        let location_a_data = Location {
            account_type: doublezero_serviceability::state::accounttype::AccountType::Location,
            owner: agent_keypair.pubkey(),
            index: 1,
            bump_seed: 0,
            code: "LOC_A".to_string(),
            name: "Location A".to_string(),
            country: "US".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 1,
            status: LocationStatus::Activated,
        };
        create_mock_location_account(
            &mut program_test,
            &serviceability_program_id,
            &location_a_pk,
            &location_a_data,
        );

        let location_z_data = Location {
            account_type: doublezero_serviceability::state::accounttype::AccountType::Location,
            owner: agent_keypair.pubkey(),
            index: 2,
            bump_seed: 0,
            code: "LOC_Z".to_string(),
            name: "Location Z".to_string(),
            country: "US".to_string(),
            lat: 1.0,
            lng: 1.0,
            loc_id: 2,
            status: LocationStatus::Activated,
        };
        create_mock_location_account(
            &mut program_test,
            &serviceability_program_id,
            &location_z_pk,
            &location_z_data,
        );

        let (mut banks_client, payer, recent_blockhash) = program_test.start().await;

        // Fund the agent keypair
        fund_account(
            &mut banks_client,
            &payer,
            &agent_keypair.pubkey(),
            1_000_000_000,
            recent_blockhash,
        )
        .await
        .unwrap();

        let (latency_pda, _bump) = derive_thirdparty_latency_samples_pda(
            &telemetry_program_id,
            &data_provider_name_bytes,
            &location_a_pk,
            &location_z_pk,
            epoch,
        );

        let args = InitializeThirdPartyLatencySamplesArgs {
            data_provider_name: data_provider_name_bytes,
            location_a_index: 1, // Not used by program, but part of args
            location_z_index: 2,
            epoch,
        };
        let instruction_data = TelemetryInstruction::InitializeThirdPartyLatencySamples(args)
            .pack()
            .unwrap();

        let accounts = vec![
            AccountMeta::new(latency_pda, false),
            AccountMeta::new_readonly(location_a_pk, false),
            AccountMeta::new_readonly(location_z_pk, false),
            AccountMeta::new(agent_keypair.pubkey(), true),
            AccountMeta::new_readonly(system_program::id(), false),
            AccountMeta::new_readonly(serviceability_program_id, false),
        ];

        let result = execute_transaction(
            &mut banks_client,
            &agent_keypair,
            recent_blockhash,
            telemetry_program_id,
            instruction_data,
            accounts,
        )
        .await;
        assert!(result.is_ok(), "Transaction failed: {:?}", result.err());

        let account_data_raw = get_account_data(&mut banks_client, latency_pda)
            .await
            .expect("Latency PDA not found");
        assert_eq!(account_data_raw.owner, telemetry_program_id);
        assert_eq!(
            account_data_raw.data.len(),
            THIRDPARTY_LATENCY_SAMPLES_MAX_SIZE
        );

        let samples_account_data =
            ThirdPartyLatencySamples::try_from_slice(&account_data_raw.data).unwrap();
        assert_eq!(
            samples_account_data.account_type,
            AccountType::ThirdPartyLatencySamples
        );
        assert_eq!(
            samples_account_data.data_provider_name,
            data_provider_name_bytes
        );
        assert_eq!(samples_account_data.epoch, epoch);
        assert_eq!(samples_account_data.location_a_pk, location_a_pk);
        assert_eq!(samples_account_data.location_z_pk, location_z_pk);
        assert_eq!(samples_account_data.start_timestamp_microseconds, 0);
        assert_eq!(samples_account_data.next_sample_index, 0);
        assert_eq!(samples_account_data.agent_pk, agent_keypair.pubkey());
        assert_eq!(samples_account_data.samples.capacity(), MAX_SAMPLES);
        assert_eq!(samples_account_data.samples.len(), 0);
    }

    // Add tests for failure conditions:
    // - test_initialize_thirdparty_latency_samples_fail_account_already_exists()
}
