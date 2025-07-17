#[cfg(test)]
mod device_test {
    use crate::{
        entrypoint::*,
        instructions::*,
        pda::*,
        processors::{
            device::{closeaccount::*, create::*, delete::*, resume::*, suspend::*, update::*},
            *,
        },
        state::{accounttype::AccountType, device::*},
        tests::test::*,
    };
    use device::activate::DeviceActivateArgs;
    use globalconfig::set::SetGlobalConfigArgs;
    use solana_program_test::*;
    use solana_sdk::{hash::Hash, instruction::AccountMeta, pubkey::Pubkey, signature::Keypair};

    #[tokio::test]
    async fn test_device() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "doublezero_serviceability",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        /***********************************************************************************************************************************/
        println!("🟢  Start test_device");

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        println!("🟢 1. Global Initialization...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::InitGlobalState(),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            &payer,
        )
        .await;

        let (config_pubkey, _) = get_globalconfig_pda(&program_id);
        println!("🟢 2. Set GlobalConfig...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
                local_asn: 65000,
                remote_asn: 65001,
                tunnel_tunnel_block: ([10, 0, 0, 0], 24),
                user_tunnel_block: ([10, 0, 0, 0], 24),
                multicastgroup_block: ([224, 0, 0, 0], 4),
            }),
            vec![
                AccountMeta::new(config_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        println!("🟢 3. Create Location...");
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 0);

        let (location_pubkey, bump_seed) =
            get_location_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "la".to_string(),
                name: "Los Angeles".to_string(),
                country: "us".to_string(),
                lat: 1.234,
                lng: 4.567,
                loc_id: 0,
            }),
            vec![
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        println!("🟢 4. Create Exchange...");

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 1);

        let (exchange_pubkey, bump_seed) =
            get_exchange_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "la".to_string(),
                name: "Los Angeles".to_string(),
                lat: 1.234,
                lng: 4.567,
                loc_id: 0,
            }),
            vec![
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        // Device _la
        println!("🟢 5. Create Device...");

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 2);

        let (device_pubkey, bump_seed) =
            get_device_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "la".to_string(),
                device_type: DeviceType::Switch,
                location_pk: location_pubkey,
                exchange_pk: exchange_pubkey,
                public_ip: [10, 0, 0, 1],
                dz_prefixes: vec![([10, 1, 0, 0], 23)],
                metrics_publisher_pk: Pubkey::default(),
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let device = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device()
            .unwrap();
        assert_eq!(device.account_type, AccountType::Device);
        assert_eq!(device.code, "la".to_string());
        assert_eq!(device.status, DeviceStatus::Pending);

        println!("✅ Device initialized successfully",);
        /*****************************************************************************************************************************************************/
        println!("🟢 6. Activate Device...");

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs {
                index: device.index,
                bump_seed: device.bump_seed,
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let device = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device()
            .unwrap();
        assert_eq!(device.account_type, AccountType::Device);
        assert_eq!(device.code, "la".to_string());
        assert_eq!(device.status, DeviceStatus::Activated);

        println!("✅ Link updated");
        /*****************************************************************************************************************************************************/
        println!("🟢 7. Suspend Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SuspendDevice(DeviceSuspendArgs {
                index: device.index,
                bump_seed: device.bump_seed,
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let device_la = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device()
            .unwrap();
        assert_eq!(device_la.account_type, AccountType::Device);
        assert_eq!(device_la.status, DeviceStatus::Suspended);

        println!("✅ Device suspended");
        /*****************************************************************************************************************************************************/
        println!("🟢 8. Resume Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ResumeDevice(DeviceResumeArgs {
                index: device_la.index,
                bump_seed: device_la.bump_seed,
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let device = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device()
            .unwrap();
        assert_eq!(device.account_type, AccountType::Device);
        assert_eq!(device.status, DeviceStatus::Activated);

        println!("✅ Device resumed");
        /*****************************************************************************************************************************************************/
        println!("🟢 9. Update Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                index: device.index,
                bump_seed: device.bump_seed,
                code: Some("la2".to_string()),
                device_type: Some(DeviceType::Switch),
                public_ip: Some([10, 2, 2, 1]),
                dz_prefixes: Some(vec![([10, 1, 0, 0], 23)]),
                metrics_publisher_pk: Some(Pubkey::default()),
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let device_la = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device()
            .unwrap();
        assert_eq!(device_la.account_type, AccountType::Device);
        assert_eq!(device_la.code, "la2".to_string());
        assert_eq!(device_la.public_ip, [10, 2, 2, 1]);
        assert_eq!(device_la.status, DeviceStatus::Activated);

        println!("✅ Device updated");
        /*****************************************************************************************************************************************************/
        println!("🟢 10. Deleting Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs {
                index: device_la.index,
                bump_seed: device_la.bump_seed,
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let device_la = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device()
            .unwrap();
        assert_eq!(device_la.account_type, AccountType::Device);
        assert_eq!(device_la.code, "la2".to_string());
        assert_eq!(device_la.public_ip, [10, 2, 2, 1]);
        assert_eq!(device_la.status, DeviceStatus::Deleting);

        /*****************************************************************************************************************************************************/
        println!("🟢 11. CloseAccount Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CloseAccountDevice(DeviceCloseAccountArgs {
                index: device.index,
                bump_seed: device.bump_seed,
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(device.owner, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let device_la = get_account_data(&mut banks_client, device_pubkey).await;
        assert_eq!(device_la, None);

        println!("✅ Device deleted successfully");
        println!("🟢🟢🟢  End test_device  🟢🟢🟢");
    }

    #[tokio::test]
    async fn test_device_update_metrics_publisher_by_foundation_allowlist_account() {
        let (
            mut banks_client,
            payer,
            program_id,
            _globalstate_pubkey,
            location_pubkey,
            exchange_pubkey,
        ) = setup_program_with_location_and_exchange().await;

        let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

        // Create device
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 3);
        let (device_pubkey, bump_seed) =
            get_device_pda(&program_id, globalstate_account.account_index + 1);
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "la".to_string(),
                device_type: DeviceType::Switch,
                location_pk: location_pubkey,
                exchange_pk: exchange_pubkey,
                public_ip: [10, 0, 0, 1],
                dz_prefixes: vec![([10, 1, 0, 0], 23)],
                metrics_publisher_pk: Pubkey::default(),
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;
        let device = get_account_data(&mut banks_client, device_pubkey)
            .await
            .unwrap()
            .get_device()
            .unwrap();
        assert_eq!(device.account_type, AccountType::Device);
        assert_eq!(device.code, "la".to_string());
        assert_eq!(device.status, DeviceStatus::Pending);

        // Update device metrics publisher by foundation allowlist account (payer)
        let metrics_publisher_pk = Pubkey::new_unique();
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                index: device.index,
                bump_seed: device.bump_seed,
                code: None,
                device_type: None,
                public_ip: None,
                dz_prefixes: None,
                metrics_publisher_pk: Some(metrics_publisher_pk),
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;
        let device = get_account_data(&mut banks_client, device_pubkey)
            .await
            .unwrap()
            .get_device()
            .unwrap();
        assert_eq!(device.account_type, AccountType::Device);
        assert_eq!(device.code, "la".to_string());
        assert_eq!(device.public_ip, [10, 0, 0, 1]);
        assert_eq!(device.metrics_publisher_pk, metrics_publisher_pk);
    }

    async fn setup_program_with_location_and_exchange(
    ) -> (BanksClient, Keypair, Pubkey, Pubkey, Pubkey, Pubkey) {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "doublezero_serviceability",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::InitGlobalState(),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            &payer,
        )
        .await;

        let (config_pubkey, _) = get_globalconfig_pda(&program_id);
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
                local_asn: 65000,
                remote_asn: 65001,
                tunnel_tunnel_block: ([10, 0, 0, 0], 24),
                user_tunnel_block: ([10, 0, 0, 0], 24),
                multicastgroup_block: ([224, 0, 0, 0], 4),
            }),
            vec![
                AccountMeta::new(config_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 0);

        let (location_pubkey, bump_seed) =
            get_location_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "la".to_string(),
                name: "Los Angeles".to_string(),
                country: "us".to_string(),
                lat: 1.234,
                lng: 4.567,
                loc_id: 0,
            }),
            vec![
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 1);

        let (exchange_pubkey, bump_seed) =
            get_exchange_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "la".to_string(),
                name: "Los Angeles".to_string(),
                lat: 1.234,
                lng: 4.567,
                loc_id: 0,
            }),
            vec![
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 2);

        let (device_pubkey, bump_seed) =
            get_device_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "la".to_string(),
                device_type: DeviceType::Switch,
                location_pk: location_pubkey,
                exchange_pk: exchange_pubkey,
                public_ip: [10, 0, 0, 1],
                dz_prefixes: vec![([10, 1, 0, 0], 23)],
                metrics_publisher_pk: Pubkey::default(),
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let device = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device()
            .unwrap();
        assert_eq!(device.account_type, AccountType::Device);
        assert_eq!(device.code, "la".to_string());
        assert_eq!(device.status, DeviceStatus::Pending);

        (
            banks_client,
            payer,
            program_id,
            globalstate_pubkey,
            location_pubkey,
            exchange_pubkey,
        )
    }

    async fn wait_for_new_blockhash(banks_client: &mut BanksClient) -> Hash {
        let current_blockhash = banks_client.get_latest_blockhash().await.unwrap();

        let mut new_blockhash = current_blockhash;
        while new_blockhash == current_blockhash {
            new_blockhash = banks_client.get_latest_blockhash().await.unwrap();
            tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
        }

        new_blockhash
    }
}
