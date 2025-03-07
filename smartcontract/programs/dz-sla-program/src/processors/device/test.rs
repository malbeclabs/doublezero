#[cfg(test)]
mod device_test {
    use crate::entrypoint::*;
    use crate::instructions::*;
    use crate::pda::*;
    use crate::processors::device::{
        create::*, deactivate::*, delete::*, reactivate::*, suspend::*, update::*,
    };
    use crate::processors::*;
    use crate::state::accounttype::AccountType;
    use crate::state::device::*;
    use crate::tests::test::*;
    use device::activate::DeviceActivateArgs;
    use globalconfig::set::SetGlobalConfigArgs;
    use solana_program_test::*;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

    #[tokio::test]
    async fn test_device() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "double_zero_sla_program",
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

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
                local_asn: 65000,
                remote_asn: 65001,
                tunnel_tunnel_block: ([10, 0, 0, 0], 24),
                user_tunnel_block: ([10, 0, 0, 0], 24),
            }),
            vec![AccountMeta::new(config_pubkey, false)],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        println!("🟢 2. Create Location...");
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 0);

        let (location_pubkey, _) =
            get_location_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
                index: globalstate_account.account_index + 1,
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
        println!("🟢 3. Create Exchange...");

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 1);

        let (exchange_pubkey, _) =
            get_exchange_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
                index: globalstate_account.account_index + 1,
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
        println!("🟢 4. Create Device...");

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 2);

        let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                index: globalstate_account.account_index + 1,
                code: "la".to_string(),
                device_type: DeviceType::Switch,
                location_pk: location_pubkey,
                exchange_pk: exchange_pubkey,
                public_ip: [10, 0, 0, 1],
                dz_prefix: ([10, 1, 0, 0], 23),
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
            .get_device();
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
            .get_device();
        assert_eq!(device.account_type, AccountType::Device);
        assert_eq!(device.code, "la".to_string());
        assert_eq!(device.status, DeviceStatus::Activated);

        println!("✅ Tunnel updated");
        /*****************************************************************************************************************************************************/
        println!("🟢 5. Suspend Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SuspendDevice(DeviceSuspendArgs {
                index: device.index,
            }),
            vec![AccountMeta::new(device_pubkey, false)],
            &payer,
        )
        .await;

        let device_la = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device();
        assert_eq!(device_la.account_type, AccountType::Device);
        assert_eq!(device_la.status, DeviceStatus::Suspended);

        println!("✅ Device suspended");
        /*****************************************************************************************************************************************************/
        println!("🟢 6. Reactivate Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ReactivateDevice(DeviceReactivateArgs {
                index: device_la.index,
            }),
            vec![AccountMeta::new(device_pubkey, false)],
            &payer,
        )
        .await;

        let device = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device();
        assert_eq!(device.account_type, AccountType::Device);
        assert_eq!(device.status, DeviceStatus::Activated);

        println!("✅ Device reactivated");
        /*****************************************************************************************************************************************************/
        println!("🟢 7. Update Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                index: device.index,
                code: Some("la2".to_string()),
                device_type: Some(DeviceType::Switch),
                public_ip: Some([10, 2, 2, 1]),
                dz_prefix: Some(([10, 1, 0, 0], 23)),
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
            .get_device();
        assert_eq!(device_la.account_type, AccountType::Device);
        assert_eq!(device_la.code, "la2".to_string());
        assert_eq!(device_la.public_ip, [10, 2, 2, 1]);
        assert_eq!(device_la.status, DeviceStatus::Activated);

        println!("✅ Device updated");
        /*****************************************************************************************************************************************************/
        println!("🟢 8. Deleting Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs {
                index: device_la.index,
            }),
            vec![AccountMeta::new(device_pubkey, false)],
            &payer,
        )
        .await;

        let device_la = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device();
        assert_eq!(device_la.account_type, AccountType::Device);
        assert_eq!(device_la.code, "la2".to_string());
        assert_eq!(device_la.public_ip, [10, 2, 2, 1]);
        assert_eq!(device_la.status, DeviceStatus::Deleting);

        /*****************************************************************************************************************************************************/
        println!("🟢 9. Deactivate Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::DeactivateDevice(DeviceDeactivateArgs {
                index: device.index,
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
}
