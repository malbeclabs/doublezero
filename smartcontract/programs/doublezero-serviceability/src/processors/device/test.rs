#[cfg(test)]
mod device_test {
    use crate::{
        entrypoint::*,
        instructions::*,
        pda::*,
        processors::{
            contributor::create::ContributorCreateArgs,
            device::{closeaccount::*, create::*, delete::*, resume::*, suspend::*, update::*},
            *,
        },
        state::{accounttype::AccountType, contributor::ContributorStatus, device::*},
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
        let (program_config_pubkey, _) = get_program_config_pda(&program_id);
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        /***********************************************************************************************************************************/
        println!("🟢 1. Global Initialization...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::InitGlobalState(),
            vec![
                AccountMeta::new(program_config_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        println!("🟢 2. Set GlobalConfig...");
        let (config_pubkey, _) = get_globalconfig_pda(&program_id);
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

        let (location_pubkey, _) =
            get_location_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
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

        let (exchange_pubkey, _) =
            get_exchange_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
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
        println!("🟢 5. Create Contributor...");
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 2);

        let (contributor_pubkey, bump_seed) =
            get_contributor_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "cont".to_string(),
            }),
            vec![
                AccountMeta::new(contributor_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let contributor = get_account_data(&mut banks_client, contributor_pubkey)
            .await
            .expect("Unable to get Account")
            .get_contributor()
            .unwrap();
        assert_eq!(contributor.account_type, AccountType::Contributor);
        assert_eq!(contributor.code, "cont".to_string());
        assert_eq!(contributor.status, ContributorStatus::Activated);

        println!("✅ Contributor initialized successfully",);
        /***********************************************************************************************************************************/
        // Device _la
        println!("🟢 6. Create Device...");
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 3);

        let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                code: "la".to_string(),
                device_type: DeviceType::Switch,
                contributor_pk: contributor_pubkey,
                location_pk: location_pubkey,
                exchange_pk: exchange_pubkey,
                public_ip: [10, 0, 0, 1].into(),
                dz_prefixes: "10.1.0.0/23".parse().unwrap(),
                metrics_publisher_pk: Pubkey::default(),
                bgp_asn: 42,
                dia_bgp_asn: 4242,
                mgmt_vrf: "mgmt".to_string(),
                dns_servers: vec![[8, 8, 8, 8].into(), [8, 8, 4, 4].into()],
                ntp_servers: vec![[192, 168, 1, 1].into(), [192, 168, 1, 2].into()],
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(contributor_pubkey, false),
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
        println!("🟢 7. Activate Device...");

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
        println!("🟢 8. Suspend Device...");
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
        println!("🟢 9. Resume Device...");
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
        println!("🟢 10. Update Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
                code: Some("la2".to_string()),
                device_type: Some(DeviceType::Switch),
                contributor_pk: None,
                public_ip: Some([10, 2, 2, 1].into()),
                dz_prefixes: Some("10.1.0.0/23".parse().unwrap()),
                metrics_publisher_pk: Some(Pubkey::default()),
                bgp_asn: Some(42),
                dia_bgp_asn: Some(4242),
                mgmt_vrf: Some("mgmt".to_string()),
                dns_servers: Some(vec![[8, 8, 8, 8].into(), [8, 8, 4, 4].into()]),
                ntp_servers: Some(vec![[192, 168, 1, 1].into(), [192, 168, 1, 2].into()]),
                interfaces: None,
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
        println!("🟢 11. Deleting Device...");
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
        println!("🟢 12. CloseAccount Device...");
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

    #[tokio::test]
    async fn test_device_update_metrics_publisher_by_foundation_allowlist_account() {
        let (
            mut banks_client,
            payer,
            program_id,
            _globalstate_pubkey,
            location_pubkey,
            exchange_pubkey,
            contributor_pubkey,
        ) = setup_program_with_location_and_exchange().await;

        let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;

        // Create device
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 3);
        let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                code: "la".to_string(),
                device_type: DeviceType::Switch,
                contributor_pk: contributor_pubkey,
                location_pk: location_pubkey,
                exchange_pk: exchange_pubkey,
                public_ip: [10, 0, 0, 1].into(),
                dz_prefixes: "10.1.0.0/23".parse().unwrap(),
                metrics_publisher_pk: Pubkey::default(),
                bgp_asn: 42,
                dia_bgp_asn: 4242,
                mgmt_vrf: "mgmt".to_string(),
                dns_servers: vec![[8, 8, 8, 8].into()],
                ntp_servers: vec![[192, 168, 1, 1].into()],
            }),
            vec![
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(contributor_pubkey, false),
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
                code: None,
                device_type: None,
                contributor_pk: None,
                public_ip: None,
                dz_prefixes: None,
                metrics_publisher_pk: Some(metrics_publisher_pk),
                bgp_asn: None,
                dia_bgp_asn: None,
                mgmt_vrf: None,
                dns_servers: None,
                ntp_servers: None,
                interfaces: None,
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
        assert_eq!(device.public_ip.to_string(), "10.0.0.1");
        assert_eq!(device.metrics_publisher_pk, metrics_publisher_pk);
    }

    async fn setup_program_with_location_and_exchange(
    ) -> (BanksClient, Keypair, Pubkey, Pubkey, Pubkey, Pubkey, Pubkey) {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "doublezero_serviceability",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        // Start with a fresh program
        let (program_config_pubkey, _) = get_program_config_pda(&program_id);
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::InitGlobalState(),
            vec![
                AccountMeta::new(program_config_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        // Initialize GlobalConfig
        let (config_pubkey, _) = get_globalconfig_pda(&program_id);
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

        // Create Location
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 0);

        let (location_pubkey, _) =
            get_location_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
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

        // Create Exchange
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 1);

        let (exchange_pubkey, _) =
            get_exchange_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
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

        // Create Contributor
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 2);

        let (contributor_pubkey, bump_seed) =
            get_contributor_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "cont".to_string(),
            }),
            vec![
                AccountMeta::new(contributor_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let contributor = get_account_data(&mut banks_client, contributor_pubkey)
            .await
            .expect("Unable to get Account")
            .get_contributor()
            .unwrap();
        assert_eq!(contributor.account_type, AccountType::Contributor);
        assert_eq!(contributor.code, "cont".to_string());
        assert_eq!(contributor.status, ContributorStatus::Activated);

        (
            banks_client,
            payer,
            program_id,
            globalstate_pubkey,
            location_pubkey,
            exchange_pubkey,
            contributor_pubkey,
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
