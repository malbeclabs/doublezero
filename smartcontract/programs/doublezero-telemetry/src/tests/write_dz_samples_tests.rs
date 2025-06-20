#[cfg(test)]
mod tests {
    use crate::{
        constants::DEFAULT_SAMPLING_INTERVAL_MICROSECONDS, instructions::TelemetryInstruction,
        pda::derive_dz_latency_samples_pda,
        processors::telemetry::write_dz_samples::WriteDzLatencySamplesArgs,
        state::dz_latency_samples::DzLatencySamples, tests::test_helpers::*,
    };
    use borsh::BorshDeserialize;
    use doublezero_serviceability::state::{
        device::{Device, DeviceStatus, DeviceType},
        link::{Link, LinkLinkType, LinkStatus},
    };
    use solana_program_test::*;
    use solana_sdk::{
        instruction::AccountMeta,
        pubkey::Pubkey,
        signature::{Keypair, Signer},
        transaction::Transaction,
    };

    #[tokio::test]
    async fn test_write_dz_latency_samples_success() {
        let mut program_test = ProgramTest::default();
        let (telemetry_program_id, serviceability_program_id) =
            add_programs_for_telemetry_tests(&mut program_test);

        // Setup test data before starting the test
        let agent_keypair = Keypair::new();
        let device_a_pk = Pubkey::new_unique();
        let device_z_pk = Pubkey::new_unique();
        let link_pk = Pubkey::new_unique();
        let epoch = 1u64;

        // Create mock serviceability accounts
        let device_a_data = Device {
            account_type: doublezero_serviceability::state::accounttype::AccountType::Device,
            owner: agent_keypair.pubkey(), // Using agent as owner for this test
            index: 1,
            bump_seed: 0,
            code: "DEV_A".to_string(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [1; 4],
            status: DeviceStatus::Activated,
            dz_prefixes: vec![],
            metrics_publisher_pk: agent_keypair.pubkey(),
        };

        let device_z_data = Device {
            account_type: doublezero_serviceability::state::accounttype::AccountType::Device,
            owner: agent_keypair.pubkey(),
            index: 2,
            bump_seed: 0,
            code: "DEV_Z".to_string(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [2; 4],
            status: DeviceStatus::Activated,
            dz_prefixes: vec![],
            metrics_publisher_pk: Pubkey::new_unique(),
        };

        let link_data = Link {
            account_type: doublezero_serviceability::state::accounttype::AccountType::Link,
            owner: agent_keypair.pubkey(),
            index: 3,
            bump_seed: 0,
            code: "LINK1".to_string(),
            side_a_pk: device_a_pk,
            side_z_pk: device_z_pk,
            link_type: LinkLinkType::L3,
            bandwidth: 1000,
            mtu: 1500,
            delay_ns: 10,
            jitter_ns: 1,
            tunnel_id: 1,
            tunnel_net: ([0; 4], 0),
            status: LinkStatus::Activated,
        };

        // Add mock accounts to program test
        create_mock_device_account(
            &mut program_test,
            &serviceability_program_id,
            &device_a_pk,
            &device_a_data,
        );
        create_mock_device_account(
            &mut program_test,
            &serviceability_program_id,
            &device_z_pk,
            &device_z_data,
        );
        create_mock_link_account(
            &mut program_test,
            &serviceability_program_id,
            &link_pk,
            &link_data,
        );

        // Start the test
        let (mut banks_client, payer, recent_blockhash) = program_test.start().await;

        // Transfer some SOL to the agent keypair so it can pay for transactions
        let transfer_instruction = solana_sdk::system_instruction::transfer(
            &payer.pubkey(),
            &agent_keypair.pubkey(),
            1_000_000_000, // 1 SOL
        );
        let mut transfer_transaction =
            Transaction::new_with_payer(&[transfer_instruction], Some(&payer.pubkey()));
        transfer_transaction.sign(&[&payer], recent_blockhash);
        banks_client
            .process_transaction(transfer_transaction)
            .await
            .unwrap();

        // Initialize DZ latency samples account
        let (latency_pda, _bump) = derive_dz_latency_samples_pda(
            &telemetry_program_id,
            &device_a_pk,
            &device_z_pk,
            &link_pk,
            epoch,
        );

        println!("Telemetry program ID: {}", telemetry_program_id);
        println!("Serviceability program ID: {}", serviceability_program_id);
        println!("Device A PK: {}", device_a_pk);
        println!("Device Z PK: {}", device_z_pk);
        println!("Link PK: {}", link_pk);
        println!("Latency PDA: {}", latency_pda);
        println!("Agent keypair: {}", agent_keypair.pubkey());

        // Check if the accounts exist
        let device_a_account = banks_client.get_account(device_a_pk).await.unwrap();
        println!("Device A account exists: {}", device_a_account.is_some());
        let device_z_account = banks_client.get_account(device_z_pk).await.unwrap();
        println!("Device Z account exists: {}", device_z_account.is_some());
        let link_account = banks_client.get_account(link_pk).await.unwrap();
        println!("Link account exists: {}", link_account.is_some());
        let pda_account = banks_client.get_account(latency_pda).await.unwrap();
        println!("PDA account exists: {}", pda_account.is_some());

        let init_args =
            crate::processors::telemetry::initialize_dz_samples::InitializeDzLatencySamplesArgs {
                device_a_index: 1,
                device_z_index: 2,
                link_index: 3,
                epoch,
                sampling_interval_microseconds: DEFAULT_SAMPLING_INTERVAL_MICROSECONDS,
            };
        let init_instruction_data = TelemetryInstruction::InitializeDzLatencySamples(init_args)
            .pack()
            .unwrap();
        let init_accounts = vec![
            AccountMeta::new(latency_pda, false),
            AccountMeta::new_readonly(device_a_pk, false),
            AccountMeta::new_readonly(device_z_pk, false),
            AccountMeta::new_readonly(link_pk, false),
            AccountMeta::new(agent_keypair.pubkey(), true),
            AccountMeta::new_readonly(solana_sdk::system_program::id(), false),
            AccountMeta::new_readonly(serviceability_program_id, false),
        ];
        execute_transaction(
            &mut banks_client,
            &agent_keypair,
            recent_blockhash,
            telemetry_program_id,
            init_instruction_data,
            init_accounts,
        )
        .await
        .unwrap();

        let samples_to_write = vec![1000, 1200, 1100];
        let current_timestamp = 1_700_000_000_000_100; // Example timestamp

        let write_args = WriteDzLatencySamplesArgs {
            device_a_index: 1,
            device_z_index: 2,
            link_index: 3,
            epoch,
            start_timestamp_microseconds: current_timestamp,
            samples: samples_to_write.clone(),
        };
        let write_instruction_data = TelemetryInstruction::WriteDzLatencySamples(write_args)
            .pack()
            .unwrap();

        let write_accounts = vec![
            AccountMeta::new(latency_pda, false),
            AccountMeta::new_readonly(device_a_pk, false), // Passed for PDA derivation consistency
            AccountMeta::new_readonly(device_z_pk, false),
            AccountMeta::new_readonly(link_pk, false),
            AccountMeta::new(agent_keypair.pubkey(), true),
        ];

        let result = execute_transaction(
            &mut banks_client,
            &agent_keypair,
            recent_blockhash,
            telemetry_program_id,
            write_instruction_data,
            write_accounts,
        )
        .await;
        assert!(result.is_ok(), "Transaction failed: {:?}", result.err());

        let account_data_raw = get_account_data(&mut banks_client, latency_pda)
            .await
            .expect("Latency PDA not found");
        let samples_account_data =
            DzLatencySamples::try_from_slice(&account_data_raw.data).unwrap();

        assert_eq!(samples_account_data.samples, samples_to_write);
        assert_eq!(
            samples_account_data.next_sample_index,
            samples_to_write.len() as u32
        );
        assert_eq!(
            samples_account_data.start_timestamp_microseconds,
            current_timestamp
        );

        // Write more samples
        let more_samples = vec![1300, 1400];
        let new_timestamp = 1_700_000_000_000_200; // Later timestamp, should not overwrite original start
        let write_args_2 = WriteDzLatencySamplesArgs {
            device_a_index: 1,
            device_z_index: 2,
            link_index: 3,
            epoch,
            start_timestamp_microseconds: new_timestamp,
            samples: more_samples.clone(),
        };
        let write_instruction_data_2 = TelemetryInstruction::WriteDzLatencySamples(write_args_2)
            .pack()
            .unwrap();

        let write_accounts_2 = vec![
            AccountMeta::new(latency_pda, false),
            AccountMeta::new_readonly(device_a_pk, false),
            AccountMeta::new_readonly(device_z_pk, false),
            AccountMeta::new_readonly(link_pk, false),
            AccountMeta::new(agent_keypair.pubkey(), true),
        ];

        execute_transaction(
            &mut banks_client,
            &agent_keypair,
            recent_blockhash,
            telemetry_program_id,
            write_instruction_data_2,
            write_accounts_2,
        )
        .await
        .unwrap();

        let account_data_raw_2 = get_account_data(&mut banks_client, latency_pda)
            .await
            .expect("Latency PDA not found");
        let samples_account_data_2 =
            DzLatencySamples::try_from_slice(&account_data_raw_2.data).unwrap();

        let mut expected_samples = samples_to_write.clone();
        expected_samples.extend(more_samples);
        assert_eq!(samples_account_data_2.samples, expected_samples);
        assert_eq!(
            samples_account_data_2.next_sample_index,
            expected_samples.len() as u32
        );
        assert_eq!(
            samples_account_data_2.start_timestamp_microseconds,
            current_timestamp
        ); // Should remain original
    }

    // Add more tests for WriteDzLatencySamples failure cases:
    // - test_write_dz_latency_samples_fail_account_not_exist()
    // - test_write_dz_latency_samples_fail_unauthorized_agent()
    // - test_write_dz_latency_samples_fail_epoch_mismatch()
    // - test_write_dz_latency_samples_fail_account_full()
    // - test_write_dz_latency_samples_fail_invalid_pda_components()
}
