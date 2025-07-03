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
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

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

        let (program_config_pubkey, _) = get_program_config_pda(&program_id);
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        println!("🟢 1. Global Initialization...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::InitGlobalState,
            vec![
                AccountMeta::new(program_config_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
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
                device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
                user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
                multicastgroup_block: "224.0.0.0/4".parse().unwrap(),
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
                public_ip: [10, 0, 0, 1].into(),
                dz_prefixes: "10.1.0.0/23".parse().unwrap(),
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
            DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs),
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
            DoubleZeroInstruction::SuspendDevice(DeviceSuspendArgs {}),
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
            DoubleZeroInstruction::ResumeDevice(DeviceResumeArgs {}),
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
                code: Some("la2".to_string()),
                device_type: Some(DeviceType::Switch),
                public_ip: Some([10, 2, 2, 1].into()),
                dz_prefixes: Some("10.1.0.0/23".parse().unwrap()),
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
        assert_eq!(device_la.public_ip.to_string(), "10.2.2.1");
        assert_eq!(device_la.status, DeviceStatus::Activated);

        println!("✅ Device updated");
        /*****************************************************************************************************************************************************/
        println!("🟢 10. Deleting Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs {}),
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
        assert_eq!(device_la.public_ip.to_string(), "10.2.2.1");
        assert_eq!(device_la.status, DeviceStatus::Deleting);

        /*****************************************************************************************************************************************************/
        println!("🟢 11. CloseAccount Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CloseAccountDevice(DeviceCloseAccountArgs {}),
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
}
