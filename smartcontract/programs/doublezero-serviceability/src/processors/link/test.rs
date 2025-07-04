#[cfg(test)]
mod tunnel_test {
    use crate::{
        entrypoint::*,
        instructions::*,
        pda::*,
        processors::{
            contributor::create::ContributorCreateArgs,
            link::{activate::*, create::*, delete::*, resume::*, suspend::*, update::*},
            *,
        },
        state::{
            accounttype::AccountType, contributor::ContributorStatus, device::DeviceType, link::*,
        },
        tests::test::*,
    };
    use globalconfig::set::SetGlobalConfigArgs;
    use link::closeaccount::LinkCloseAccountArgs;
    use solana_program_test::*;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

    #[tokio::test]
    async fn test_link() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "doublezero_serviceability",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        /***********************************************************************************************************************************/
        println!("🟢  Start test_link");

        let (program_config_pubkey, _) = get_program_config_pda(&program_id);
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

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
                multicastgroup_block: "10.0.0.0/24".parse().unwrap(),
            }),
            vec![
                AccountMeta::new(config_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        println!("🟢 2. Create Location...");
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
        println!("🟢 3. Create Exchange...");

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
                ata_owner_pk: Pubkey::default(),
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
        println!("🟢 3. Create Device...");

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 3);

        let (device_a_pubkey, bump_seed) =
            get_device_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "A".to_string(),
                device_type: DeviceType::Switch,
                contributor_pk: contributor_pubkey,
                location_pk: location_pubkey,
                exchange_pk: exchange_pubkey,
                public_ip: [10, 0, 0, 1].into(),
                dz_prefixes: "10.1.0.0/24".parse().unwrap(),
                metrics_publisher_pk: Pubkey::default(),
            }),
            vec![
                AccountMeta::new(device_a_pubkey, false),
                AccountMeta::new(contributor_pubkey, false),
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        println!("🟢 4. Create Device...");

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 4);

        let (device_z_pubkey, bump_seed) =
            get_device_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "Z".to_string(),
                device_type: DeviceType::Switch,
                contributor_pk: contributor_pubkey,
                location_pk: location_pubkey,
                exchange_pk: exchange_pubkey,
                public_ip: [11, 0, 0, 1].into(),
                dz_prefixes: "11.1.0.0/23".parse().unwrap(),
                metrics_publisher_pk: Pubkey::default(),
            }),
            vec![
                AccountMeta::new(device_z_pubkey, false),
                AccountMeta::new(contributor_pubkey, false),
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        /***********************************************************************************************************************************/
        // Link _la
        println!("🟢 5. Create Link...");

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 5);

        let (tunnel_pubkey, bump_seed) =
            get_link_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "la".to_string(),
                link_type: LinkLinkType::L3,
                side_a_pk: device_a_pubkey,
                side_z_pk: device_z_pubkey,
                bandwidth: 100000000,
                mtu: 9000,
                delay_ns: 150000,
                jitter_ns: 5000,
            }),
            vec![
                AccountMeta::new(tunnel_pubkey, false),
                AccountMeta::new(device_a_pubkey, false),
                AccountMeta::new(device_z_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
            .await
            .expect("Unable to get Account")
            .get_tunnel()
            .unwrap();
        assert_eq!(tunnel_la.account_type, AccountType::Link);
        assert_eq!(tunnel_la.code, "la".to_string());
        assert_eq!(tunnel_la.status, LinkStatus::Pending);

        println!("✅ Link initialized successfully",);
        /*****************************************************************************************************************************************************/
        println!("🟢 6. Activate Link...");

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                tunnel_id: 500,
                tunnel_net: "10.0.0.0/21".parse().unwrap(),
            }),
            vec![
                AccountMeta::new(tunnel_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
            .await
            .expect("Unable to get Account")
            .get_tunnel()
            .unwrap();
        assert_eq!(tunnel_la.account_type, AccountType::Link);
        assert_eq!(tunnel_la.tunnel_id, 500);
        assert_eq!(tunnel_la.tunnel_net.to_string(), "10.0.0.0/21");
        assert_eq!(tunnel_la.status, LinkStatus::Activated);

        println!("✅ Link activated");
        /*****************************************************************************************************************************************************/
        println!("🟢 7. Suspend Link...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SuspendLink(LinkSuspendArgs {}),
            vec![AccountMeta::new(tunnel_pubkey, false)],
            &payer,
        )
        .await;

        let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
            .await
            .expect("Unable to get Account")
            .get_tunnel()
            .unwrap();
        assert_eq!(tunnel_la.account_type, AccountType::Link);
        assert_eq!(tunnel_la.status, LinkStatus::Suspended);

        println!("✅ Link suspended");
        /*****************************************************************************************************************************************************/
        println!("🟢 8. Resume Link...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ResumeLink(LinkResumeArgs {}),
            vec![AccountMeta::new(tunnel_pubkey, false)],
            &payer,
        )
        .await;

        let tunnel = get_account_data(&mut banks_client, tunnel_pubkey)
            .await
            .expect("Unable to get Account")
            .get_tunnel()
            .unwrap();
        assert_eq!(tunnel.account_type, AccountType::Link);
        assert_eq!(tunnel.status, LinkStatus::Activated);

        println!("✅ Link resumed");
        /*****************************************************************************************************************************************************/
        println!("🟢 9. Update Link...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
                code: Some("la2".to_string()),
                tunnel_type: Some(LinkLinkType::L3),
                bandwidth: Some(2000000000),
                mtu: Some(8900),
                delay_ns: Some(15000),
                jitter_ns: Some(5000),
            }),
            vec![
                AccountMeta::new(tunnel_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
            .await
            .expect("Unable to get Account")
            .get_tunnel()
            .unwrap();
        assert_eq!(tunnel_la.account_type, AccountType::Link);
        assert_eq!(tunnel_la.code, "la2".to_string());
        assert_eq!(tunnel_la.bandwidth, 2000000000);
        assert_eq!(tunnel_la.mtu, 8900);
        assert_eq!(tunnel_la.delay_ns, 15000);
        assert_eq!(tunnel_la.status, LinkStatus::Activated);

        println!("✅ Link updated");

        /*****************************************************************************************************************************************************/
        println!("🟢 9. Deleting Link...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {}),
            vec![
                AccountMeta::new(tunnel_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey)
            .await
            .expect("Unable to get Account")
            .get_tunnel()
            .unwrap();
        assert_eq!(tunnel_la.account_type, AccountType::Link);
        assert_eq!(tunnel_la.code, "la2".to_string());
        assert_eq!(tunnel_la.bandwidth, 2000000000);
        assert_eq!(tunnel_la.mtu, 8900);
        assert_eq!(tunnel_la.delay_ns, 15000);
        assert_eq!(tunnel_la.status, LinkStatus::Deleting);

        println!("✅ Link deleting");

        /*****************************************************************************************************************************************************/
        println!("🟢 9. CloseAccount Link...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CloseAccountLink(LinkCloseAccountArgs {}),
            vec![
                AccountMeta::new(tunnel_pubkey, false),
                AccountMeta::new(tunnel.owner, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let tunnel_la = get_account_data(&mut banks_client, tunnel_pubkey).await;
        assert_eq!(tunnel_la, None);

        println!("✅ Link deleted successfully");
        println!("🟢🟢🟢  End test_tunnel  🟢🟢🟢");
    }
}
