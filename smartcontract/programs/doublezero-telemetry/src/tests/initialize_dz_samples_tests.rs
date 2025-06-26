#[cfg(test)]
mod tests {
    use crate::{
        error::TelemetryError, instructions::TelemetryInstruction,
        pda::derive_dz_latency_samples_pda,
        processors::telemetry::initialize_dz_samples::InitializeDzLatencySamplesArgs,
        state::dz_latency_samples::DZ_LATENCY_SAMPLES_HEADER_SIZE, tests::test_helpers::*,
    };
    use borsh::BorshSerialize;
    use doublezero_serviceability::{
        processors::{
            device::create::DeviceCreateArgs, exchange::create::ExchangeCreateArgs,
            link::create::LinkCreateArgs, location::create::LocationCreateArgs,
        },
        state::{
            accounttype::AccountType,
            device::{Device, DeviceStatus, DeviceType},
            link::{Link, LinkLinkType, LinkStatus},
        },
    };
    use solana_program::example_mocks::solana_sdk::system_program;
    use solana_program_test::*;
    use solana_sdk::{
        account::Account,
        instruction::{AccountMeta, InstructionError},
        pubkey::Pubkey,
        signature::{Keypair, Signer},
        transaction::Transaction,
    };

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

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_agent_not_signer() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        // Seed with two linked devices and a valid agent.
        let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        // Refresh blockhash.
        ledger.refresh_blockhash().await.unwrap();

        // Create PDA manually.
        let (latency_samples_pda, _) = derive_dz_latency_samples_pda(
            &ledger.telemetry.program_id,
            &origin_device_pk,
            &target_device_pk,
            &link_pk,
            1,
        );

        // Construct instruction manually with agent NOT a signer.
        let args = InitializeDzLatencySamplesArgs {
            device_a_pk: origin_device_pk,
            device_z_pk: target_device_pk,
            link_pk,
            epoch: 1,
            sampling_interval_microseconds: 5_000_000,
        };

        let instruction = TelemetryInstruction::InitializeDzLatencySamples(args.clone());
        let data = instruction.pack().unwrap();

        let accounts = vec![
            AccountMeta::new(latency_samples_pda, false),
            AccountMeta::new_readonly(origin_device_agent.pubkey(), false), // Not signer
            AccountMeta::new_readonly(origin_device_pk, false),
            AccountMeta::new_readonly(target_device_pk, false),
            AccountMeta::new_readonly(link_pk, false),
            AccountMeta::new_readonly(system_program::id(), false),
            AccountMeta::new_readonly(ledger.serviceability.program_id, false),
        ];

        let (banks_client, payer, recent_blockhash) = {
            let ctx = ledger.context.lock().unwrap();
            (
                ctx.banks_client.clone(),
                ctx.payer.insecure_clone(),
                ctx.recent_blockhash,
            )
        };

        let mut transaction = Transaction::new_with_payer(
            &[solana_sdk::instruction::Instruction {
                program_id: ledger.telemetry.program_id,
                accounts,
                data,
            }],
            Some(&payer.pubkey()),
        );

        transaction.sign(&[&payer], recent_blockhash);

        let result = banks_client.process_transaction(transaction).await;
        assert_banksclient_error(result, InstructionError::MissingRequiredSignature);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_device_a_wrong_owner() {
        let agent = Keypair::new();
        let fake_device_a_pk = Pubkey::new_unique();
        let device_z_pk = Pubkey::new_unique(); // doesn’t matter, we won’t get that far
        let link_pk = Pubkey::new_unique(); // same

        let fake_device = Device {
            index: 0,
            bump_seed: 0,
            account_type: AccountType::Device,
            code: "invalid".to_string(),
            owner: agent.pubkey(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [0, 0, 0, 0],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: agent.pubkey(),
            location_pk: Pubkey::new_unique(),
            dz_prefixes: vec![],
        };

        let mut device_data = Vec::new();
        fake_device.serialize(&mut device_data).unwrap();

        let fake_account = Account {
            lamports: 1_000_000,
            data: device_data,
            owner: Pubkey::new_unique(), // WRONG owner
            executable: false,
            rent_epoch: 0,
        };

        let mut ledger =
            LedgerHelper::new_with_preloaded_accounts(vec![(fake_device_a_pk, fake_account)])
                .await
                .unwrap();

        ledger
            .fund_account(&agent.pubkey(), 10_000_000_000)
            .await
            .unwrap();
        ledger.refresh_blockhash().await.unwrap();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(
                &agent,
                fake_device_a_pk,
                device_z_pk,
                link_pk,
                42,
                5_000_000,
            )
            .await;
        assert_banksclient_error(result, InstructionError::IncorrectProgramId);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_device_z_wrong_owner() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let (agent, device_a_pk, _real_device_z, link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        ledger.refresh_blockhash().await.unwrap();

        // Inject a fake Device Z account with wrong owner
        let fake_device_z_pk = Pubkey::new_unique();
        let wrong_owner = Pubkey::new_unique();

        let fake_device_z = Device {
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::new_unique(), // doesn't matter for Z
            location_pk: Pubkey::new_unique(),
            dz_prefixes: vec![],
            account_type: AccountType::Device,
            owner: wrong_owner,
            index: 0,
            bump_seed: 0,
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Switch,
            public_ip: [0, 0, 0, 0],
            code: "invalid".to_string(),
        };

        let mut data = Vec::new();
        fake_device_z.serialize(&mut data).unwrap();

        let fake_account = solana_sdk::account::Account {
            lamports: 1_000_000,
            data,
            owner: wrong_owner,
            executable: false,
            rent_epoch: 0,
        };

        let mut ledger =
            LedgerHelper::new_with_preloaded_accounts(vec![(fake_device_z_pk, fake_account)])
                .await
                .unwrap();

        ledger
            .fund_account(&agent.pubkey(), 10_000_000_000)
            .await
            .unwrap();
        ledger.refresh_blockhash().await.unwrap();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(
                &agent,
                device_a_pk,
                fake_device_z_pk,
                link_pk,
                88,
                5_000_000,
            )
            .await;

        assert_banksclient_error(result, InstructionError::IncorrectProgramId);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_link_wrong_owner() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let (agent, device_a_pk, device_z_pk, _real_link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        ledger.refresh_blockhash().await.unwrap();

        // Inject a fake Link account with wrong owner
        let fake_link_pk = Pubkey::new_unique();
        let wrong_owner = Pubkey::new_unique();

        let fake_link = Link {
            status: LinkStatus::Activated,
            side_a_pk: device_a_pk,
            side_z_pk: device_z_pk,
            account_type: AccountType::Link,
            owner: wrong_owner,
            index: 0,
            bump_seed: 0,
            code: "invalid".to_string(),
            bandwidth: 0,
            delay_ns: 0,
            jitter_ns: 0,
            link_type: LinkLinkType::L2,
            mtu: 0,
            tunnel_id: 0,
            tunnel_net: ([0, 0, 0, 0], 0),
        };

        let mut data = Vec::new();
        fake_link.serialize(&mut data).unwrap();

        let fake_account = solana_sdk::account::Account {
            lamports: 1_000_000,
            data,
            owner: wrong_owner,
            executable: false,
            rent_epoch: 0,
        };

        let mut ledger =
            LedgerHelper::new_with_preloaded_accounts(vec![(fake_link_pk, fake_account)])
                .await
                .unwrap();

        ledger
            .fund_account(&agent.pubkey(), 10_000_000_000)
            .await
            .unwrap();
        ledger.refresh_blockhash().await.unwrap();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(
                &agent,
                device_a_pk,
                device_z_pk,
                fake_link_pk,
                77,
                5_000_000,
            )
            .await;

        assert_banksclient_error(result, InstructionError::IncorrectProgramId);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_device_a_not_activated() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let location_pk = ledger
            .serviceability
            .create_location(LocationCreateArgs {
                code: "LOC1".to_string(),
                name: "Test Location".to_string(),
                country: "US".to_string(),
                loc_id: 1,
                ..LocationCreateArgs::default()
            })
            .await
            .unwrap();

        let exchange_pk = ledger
            .serviceability
            .create_exchange(ExchangeCreateArgs {
                code: "EX1".to_string(),
                name: "Test Exchange".to_string(),
                loc_id: 1,
                ..ExchangeCreateArgs::default()
            })
            .await
            .unwrap();

        let agent = Keypair::new();
        ledger
            .fund_account(&agent.pubkey(), 10_000_000_000)
            .await
            .unwrap();

        // Device A: not activated
        let (device_a_pk, _) = ledger
            .serviceability
            .create_device(DeviceCreateArgs {
                code: "DeviceA".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [1, 2, 3, 4],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        // Device Z: activated
        let (device_z_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "DeviceZ".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [5, 6, 7, 8],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        // Link: between device A and Z
        let (link_pk, _) = ledger
            .serviceability
            .create_and_activate_link(
                LinkCreateArgs {
                    code: "LINK1".to_string(),
                    side_a_pk: device_a_pk,
                    side_z_pk: device_z_pk,
                    link_type: LinkLinkType::L3,
                    bandwidth: 1000,
                    mtu: 1500,
                    delay_ns: 10,
                    jitter_ns: 1,
                    ..LinkCreateArgs::default()
                },
                1,
                ([10, 1, 1, 0], 30),
            )
            .await
            .unwrap();

        ledger.refresh_blockhash().await.unwrap();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(&agent, device_a_pk, device_z_pk, link_pk, 66, 5_000_000)
            .await;

        assert_telemetry_error(result, TelemetryError::DeviceNotActive);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_device_z_not_activated() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let location_pk = ledger
            .serviceability
            .create_location(LocationCreateArgs {
                code: "LOC1".to_string(),
                name: "Test Location".to_string(),
                country: "US".to_string(),
                loc_id: 1,
                ..LocationCreateArgs::default()
            })
            .await
            .unwrap();

        let exchange_pk = ledger
            .serviceability
            .create_exchange(ExchangeCreateArgs {
                code: "EX1".to_string(),
                name: "Test Exchange".to_string(),
                loc_id: 1,
                ..ExchangeCreateArgs::default()
            })
            .await
            .unwrap();

        let agent = Keypair::new();
        ledger
            .fund_account(&agent.pubkey(), 10_000_000_000)
            .await
            .unwrap();

        // Device A: activated
        let (device_a_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "DeviceA".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [1, 2, 3, 4],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        // Device Z: not activated
        let (device_z_pk, _) = ledger
            .serviceability
            .create_device(DeviceCreateArgs {
                code: "DeviceZ".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [5, 6, 7, 8],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        // Link between Device A and Device Z
        let (link_pk, _) = ledger
            .serviceability
            .create_and_activate_link(
                LinkCreateArgs {
                    code: "LINK1".to_string(),
                    side_a_pk: device_a_pk,
                    side_z_pk: device_z_pk,
                    link_type: LinkLinkType::L3,
                    bandwidth: 1000,
                    mtu: 1500,
                    delay_ns: 10,
                    jitter_ns: 1,
                    ..LinkCreateArgs::default()
                },
                1,
                ([10, 1, 1, 0], 30),
            )
            .await
            .unwrap();

        ledger.refresh_blockhash().await.unwrap();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(&agent, device_a_pk, device_z_pk, link_pk, 66, 5_000_000)
            .await;

        assert_telemetry_error(result, TelemetryError::DeviceNotActive);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_link_not_activated() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let location_pk = ledger
            .serviceability
            .create_location(LocationCreateArgs {
                code: "LOC1".to_string(),
                name: "Test Location".to_string(),
                country: "US".to_string(),
                loc_id: 1,
                ..LocationCreateArgs::default()
            })
            .await
            .unwrap();

        let exchange_pk = ledger
            .serviceability
            .create_exchange(ExchangeCreateArgs {
                code: "EX1".to_string(),
                name: "Test Exchange".to_string(),
                loc_id: 1,
                ..ExchangeCreateArgs::default()
            })
            .await
            .unwrap();

        let agent = Keypair::new();
        ledger
            .fund_account(&agent.pubkey(), 10_000_000_000)
            .await
            .unwrap();

        let (device_a_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "DeviceA".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [1, 2, 3, 4],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        let (device_z_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "DeviceZ".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [5, 6, 7, 8],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        // Create link but do not activate
        let (link_pk, _) = ledger
            .serviceability
            .create_link(LinkCreateArgs {
                code: "LINK1".to_string(),
                side_a_pk: device_a_pk,
                side_z_pk: device_z_pk,
                link_type: LinkLinkType::L3,
                bandwidth: 1000,
                mtu: 1500,
                delay_ns: 10,
                jitter_ns: 1,
                ..LinkCreateArgs::default()
            })
            .await
            .unwrap();

        ledger.refresh_blockhash().await.unwrap();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(&agent, device_a_pk, device_z_pk, link_pk, 66, 5_000_000)
            .await;

        assert_telemetry_error(result, TelemetryError::LinkNotActive);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_link_wrong_devices() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let location_pk = ledger
            .serviceability
            .create_location(LocationCreateArgs {
                code: "LOC1".to_string(),
                name: "Test Location".to_string(),
                country: "US".to_string(),
                loc_id: 1,
                ..LocationCreateArgs::default()
            })
            .await
            .unwrap();

        let exchange_pk = ledger
            .serviceability
            .create_exchange(ExchangeCreateArgs {
                code: "EX1".to_string(),
                name: "Test Exchange".to_string(),
                loc_id: 1,
                ..ExchangeCreateArgs::default()
            })
            .await
            .unwrap();

        let agent = Keypair::new();
        ledger
            .fund_account(&agent.pubkey(), 10_000_000_000)
            .await
            .unwrap();

        // Device A and Z: activated
        let (device_a_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "DeviceA".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [1, 1, 1, 1],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        let (device_z_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "DeviceZ".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [2, 2, 2, 2],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        // Other devices for the link
        let (device_x_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "DeviceX".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [3, 3, 3, 3],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        let (device_y_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "DeviceY".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [4, 4, 4, 4],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        // Link between X and Y — not A and Z
        let (link_pk, _) = ledger
            .serviceability
            .create_and_activate_link(
                LinkCreateArgs {
                    code: "LINK1".to_string(),
                    side_a_pk: device_x_pk,
                    side_z_pk: device_y_pk,
                    link_type: LinkLinkType::L2,
                    bandwidth: 1000,
                    mtu: 1500,
                    delay_ns: 10,
                    jitter_ns: 1,
                    ..LinkCreateArgs::default()
                },
                1,
                ([10, 1, 1, 0], 30),
            )
            .await
            .unwrap();

        ledger.refresh_blockhash().await.unwrap();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(&agent, device_a_pk, device_z_pk, link_pk, 55, 5_000_000)
            .await;

        assert_telemetry_error(result, TelemetryError::InvalidLink);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_succeeds_with_reversed_link_sides() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let location_pk = ledger
            .serviceability
            .create_location(LocationCreateArgs {
                code: "LOC1".into(),
                name: "Location".into(),
                country: "US".into(),
                loc_id: 1,
                ..LocationCreateArgs::default()
            })
            .await
            .unwrap();

        let exchange_pk = ledger
            .serviceability
            .create_exchange(ExchangeCreateArgs {
                code: "EX1".into(),
                name: "Exchange".into(),
                loc_id: 1,
                ..ExchangeCreateArgs::default()
            })
            .await
            .unwrap();

        let agent = Keypair::new();
        ledger
            .fund_account(&agent.pubkey(), 10_000_000_000)
            .await
            .unwrap();

        let (device_a_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "DeviceA".into(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [10, 0, 0, 1],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        let (device_z_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "DeviceZ".into(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [10, 0, 0, 2],
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        // link with device_z on side_a, device_a on side_z
        let (link_pk, _) = ledger
            .serviceability
            .create_and_activate_link(
                LinkCreateArgs {
                    code: "LINK1".into(),
                    side_a_pk: device_z_pk,
                    side_z_pk: device_a_pk,
                    link_type: LinkLinkType::L2,
                    bandwidth: 1000,
                    mtu: 1500,
                    delay_ns: 1,
                    jitter_ns: 1,
                    ..LinkCreateArgs::default()
                },
                1,
                ([192, 168, 0, 0], 24),
            )
            .await
            .unwrap();

        ledger.refresh_blockhash().await.unwrap();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(&agent, device_a_pk, device_z_pk, link_pk, 42, 5_000_000)
            .await;

        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_account_already_exists() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let (agent, device_a_pk, device_z_pk, link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        ledger.refresh_blockhash().await.unwrap();

        // First call: succeed and create the account
        let latency_samples_pda = ledger
            .telemetry
            .initialize_dz_latency_samples(
                &agent,
                device_a_pk,
                device_z_pk,
                link_pk,
                999,
                5_000_000,
            )
            .await
            .unwrap();

        // Refresh blockhash again to build new tx
        ledger.refresh_blockhash().await.unwrap();

        // Second call: explicitly pass the same latency_samples_pda as the account
        let result = ledger
            .telemetry
            .initialize_dz_latency_samples_with_pda(
                &agent,
                latency_samples_pda,
                device_a_pk,
                device_z_pk,
                link_pk,
                999,
                5_000_000,
            )
            .await;

        assert_telemetry_error(result, TelemetryError::AccountAlreadyExists);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_invalid_pda() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let (agent, device_a_pk, device_z_pk, link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        ledger.refresh_blockhash().await.unwrap();

        // Derive correct PDA (but we won't use it)
        let (_correct_pda, _bump) = derive_dz_latency_samples_pda(
            &ledger.telemetry.program_id,
            &device_a_pk,
            &device_z_pk,
            &link_pk,
            42,
        );

        // Use a wrong/fake PDA
        let fake_pda = Pubkey::new_unique();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples_with_pda(
                &agent,
                fake_pda,
                device_a_pk,
                device_z_pk,
                link_pk,
                42,
                5_000_000,
            )
            .await;

        assert_telemetry_error(result, TelemetryError::InvalidPDA);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_zero_sampling_interval() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let (agent, device_a_pk, device_z_pk, link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        ledger.refresh_blockhash().await.unwrap();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(&agent, device_a_pk, device_z_pk, link_pk, 123, 0)
            .await;

        assert_telemetry_error(result, TelemetryError::InvalidSamplingInterval);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_same_device_a_and_z() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        let (agent, device_a_pk, _device_z_pk, link_pk) =
            ledger.seed_with_two_linked_devices().await.unwrap();

        // Intentionally use device A twice
        ledger.refresh_blockhash().await.unwrap();

        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(&agent, device_a_pk, device_a_pk, link_pk, 123, 100_000)
            .await;

        assert_telemetry_error(result, TelemetryError::InvalidLink);
    }

    #[tokio::test]
    async fn test_initialize_dz_latency_samples_fail_agent_not_owner_of_device_a() {
        let mut ledger = LedgerHelper::new().await.unwrap();

        // Create agent that owns Device A
        let owner_agent = Keypair::new();
        ledger
            .fund_account(&owner_agent.pubkey(), 10_000_000_000)
            .await
            .unwrap();

        // Create a separate, unauthorized agent
        let unauthorized_agent = Keypair::new();
        ledger
            .fund_account(&unauthorized_agent.pubkey(), 10_000_000_000)
            .await
            .unwrap();

        let location_pk = ledger
            .serviceability
            .create_location(LocationCreateArgs {
                code: "LOC1".to_string(),
                name: "Loc".to_string(),
                country: "CA".to_string(),
                loc_id: 1,
                ..LocationCreateArgs::default()
            })
            .await
            .unwrap();

        let exchange_pk = ledger
            .serviceability
            .create_exchange(ExchangeCreateArgs {
                code: "EX".to_string(),
                name: "Exchange".to_string(),
                loc_id: 1,
                ..ExchangeCreateArgs::default()
            })
            .await
            .unwrap();

        // Device A: activated, owned by owner_agent
        let (device_a_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "A".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [1, 1, 1, 1],
                metrics_publisher_pk: owner_agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        // Device Z: also valid
        let (device_z_pk, _) = ledger
            .serviceability
            .create_and_activate_device(DeviceCreateArgs {
                code: "Z".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [2, 2, 2, 2],
                metrics_publisher_pk: unauthorized_agent.pubkey(),
                ..DeviceCreateArgs::default()
            })
            .await
            .unwrap();

        let (link_pk, _) = ledger
            .serviceability
            .create_and_activate_link(
                LinkCreateArgs {
                    code: "LNK".to_string(),
                    side_a_pk: device_a_pk,
                    side_z_pk: device_z_pk,
                    ..LinkCreateArgs::default()
                },
                1,
                ([10, 0, 0, 0], 24),
            )
            .await
            .unwrap();

        ledger.refresh_blockhash().await.unwrap();

        // Attempt with the unauthorized agent
        let result = ledger
            .telemetry
            .initialize_dz_latency_samples(
                &unauthorized_agent,
                device_a_pk,
                device_z_pk,
                link_pk,
                66,
                5_000_000,
            )
            .await;

        assert_telemetry_error(result, TelemetryError::UnauthorizedAgent);
    }
}
