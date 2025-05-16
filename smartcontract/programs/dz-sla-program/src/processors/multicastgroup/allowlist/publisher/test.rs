#[cfg(test)]
mod device_test {
    use crate::entrypoint::*;
    use crate::instructions::*;
    use crate::pda::*;
    use crate::processors::device::create::DeviceCreateArgs;
    use crate::processors::exchange::create::ExchangeCreateArgs;
    use crate::processors::location::create::LocationCreateArgs;
    use crate::processors::multicastgroup::activate::MulticastGroupActivateArgs;
    use crate::processors::multicastgroup::allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs;
    use crate::processors::multicastgroup::create::MulticastGroupCreateArgs;
    use crate::processors::multicastgroup::subscribe::MulticastGroupSubscribeArgs;
    use crate::processors::user::activate::UserActivateArgs;
    use crate::processors::user::create::UserCreateArgs;
    use crate::state::accounttype::AccountType;
    use crate::state::device::DeviceType;
    use crate::state::multicastgroup::MulticastGroupStatus;
    use crate::state::user::UserCYOA;
    use crate::state::user::UserStatus;
    use crate::state::user::UserType;
    use crate::tests::test::*;
    use solana_program_test::*;
    use solana_sdk::signer::Signer;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

    #[tokio::test]
    async fn multicast_publisher_allowlist_test() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "doublezero_sla_program",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        /***********************************************************************************************************************************/
        println!("🟢  Start user_allowlist_test");

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        /***********************************************************************************************************************************/
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

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 2. Create MulticastGroup...");

        let globalstate = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .expect("Unable to get Account")
            .get_global_state();

        let (multicastgroup_pubkey, bump_seed) =
            get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                index: globalstate.account_index + 1,
                bump_seed: bump_seed,
                code: "test".to_string(),
                max_bandwidth: 100,
                owner: payer.pubkey(),
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let mgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup();

        assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
        assert_eq!(mgroup.code, "test".to_string());
        assert_eq!(mgroup.status, MulticastGroupStatus::Pending);

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 3. Create MulticastGroup...");

        let (multicastgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
                index: mgroup.index,
                bump_seed: mgroup.bump_seed,
                multicast_ip: [223, 0, 0, 1],
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let mgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup();

        assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
        assert_eq!(mgroup.multicast_ip, [223, 0, 0, 1]);
        assert_eq!(mgroup.status, MulticastGroupStatus::Activated);

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 4. Create a Location ...");

        let globalstate = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .expect("Unable to get Account")
            .get_global_state();

        let (location_pubkey, bump_seed) =
            get_location_pda(&program_id, globalstate.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
                index: globalstate.account_index + 1,
                bump_seed: bump_seed,
                code: "test".to_string(),
                name: "test".to_string(),
                country: "US".to_string(),
                lat: 1.0,
                lng: 1.0,
                loc_id: 0,
            }),
            vec![
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let location = get_account_data(&mut banks_client, location_pubkey)
            .await
            .expect("Unable to get Account")
            .get_location();

        assert_eq!(location.account_type, AccountType::Location);

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 5. Create a Exchange ...");

        let globalstate = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .expect("Unable to get Account")
            .get_global_state();

        let (exchange_pubkey, bump_seed) =
            get_exchange_pda(&program_id, globalstate.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
                index: globalstate.account_index + 1,
                bump_seed: bump_seed,
                code: "test".to_string(),
                name: "test".to_string(),
                lat: 1.0,
                lng: 1.0,
                loc_id: 0,
            }),
            vec![
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let exchange = get_account_data(&mut banks_client, exchange_pubkey)
            .await
            .expect("Unable to get Account")
            .get_exchange();

        assert_eq!(exchange.account_type, AccountType::Exchange);

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 6. Create a Device connection ...");

        let globalstate = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .expect("Unable to get Account")
            .get_global_state();

        let (device_pubkey, bump_seed) = get_device_pda(&program_id, globalstate.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
                index: globalstate.account_index + 1,
                bump_seed: bump_seed,
                code: "test".to_string(),
                device_type: DeviceType::Switch,
                location_pk: location_pubkey,
                exchange_pk: exchange_pubkey,
                public_ip: [10, 0, 0, 1],
                dz_prefixes: vec![([10, 0, 0, 1], 24)],
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

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 7. Create a User connection ...");

        let globalstate = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .expect("Unable to get Account")
            .get_global_state();

        let (user_pubkey, user_bump_seed) =
            get_user_pda(&program_id, globalstate.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateUser(UserCreateArgs {
                index: globalstate.account_index + 1,
                bump_seed: user_bump_seed,
                user_type: UserType::Multicast,
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [10, 0, 0, 1],
            }),
            vec![
                AccountMeta::new(user_pubkey, false),
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let user = get_account_data(&mut banks_client, user_pubkey)
            .await
            .expect("Unable to get Account")
            .get_user();

        assert_eq!(user.account_type, AccountType::User);

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 8. Create a User connection ...");

        let globalstate = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .expect("Unable to get Account")
            .get_global_state();

        let (user_pubkey, user_bump_seed) =
            get_user_pda(&program_id, globalstate.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateUser(UserCreateArgs {
                index: globalstate.account_index + 1,
                bump_seed: user_bump_seed,
                user_type: UserType::Multicast,
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
                client_ip: [10, 0, 0, 1],
            }),
            vec![
                AccountMeta::new(user_pubkey, false),
                AccountMeta::new(device_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let user = get_account_data(&mut banks_client, user_pubkey)
            .await
            .expect("Unable to get Account")
            .get_user();

        assert_eq!(user.account_type, AccountType::User);
        assert_eq!(user.status, UserStatus::Pending);

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 9. Activate User ...");

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                index: user.index,
                bump_seed: user.bump_seed,
                tunnel_id: 1,
                tunnel_net: ([10, 0, 0, 1], 24),
                dz_ip: [10, 0, 0, 1],
            }),
            vec![
                AccountMeta::new(user_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let user = get_account_data(&mut banks_client, user_pubkey)
            .await
            .expect("Unable to get Account")
            .get_user();

        assert_eq!(user.account_type, AccountType::User);
        assert_eq!(user.status, UserStatus::Activated);

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 10. Add payer user allowlist...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                AddMulticastGroupPubAllowlistArgs {
                    pubkey: payer.pubkey(),
                },
            ),
            vec![AccountMeta::new(multicastgroup_pubkey, false)],
            &payer,
        )
        .await;

        let mgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup();

        assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
        assert!(mgroup.pub_allowlist.contains(&payer.pubkey()));

        println!("✅ Allowlist is correct");
        /*****************************************************************************************************************************************************/
        println!("🟢 11. Subscribe User to Multicast Group...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
                publisher: true,
                subscriber: false,
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(user_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let mgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup();

        assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
        assert!(mgroup.publishers.contains(&user_pubkey));

        println!("✅");
        /*****************************************************************************************************************************************************/
        /*****************************************************************************************************************************************************/
        println!("🟢 12. Unsubscribe User to Multicast Group...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SubscribeMulticastGroup(MulticastGroupSubscribeArgs {
                publisher: false,
                subscriber: false,
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(user_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let mgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup();

        assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
        assert!(!mgroup.publishers.contains(&user_pubkey));

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢🟢🟢  End test_device  🟢🟢🟢");
    }
}
