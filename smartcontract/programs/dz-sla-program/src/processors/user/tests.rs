#[cfg(test)]
mod user_test {
    use crate::entrypoint::*;
    use crate::instructions::*;
    use crate::pda::*;
    use crate::processors::user::{
        activate::*, create::*, delete::*, resume::*, suspend::*, update::*,
    };
    use crate::processors::*;

    use crate::state::accounttype::AccountType;
    use crate::state::device::*;
    use crate::state::user::UserCYOA;
    use crate::state::user::UserStatus;
    use crate::state::user::UserType;
    use crate::tests::test::*;
    use globalconfig::set::SetGlobalConfigArgs;
    use solana_program_test::*;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};
    use user::closeaccount::UserCloseAccountArgs;

    #[tokio::test]
    async fn test_user() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "doublezero_sla_program",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        /***********************************************************************************************************************************/
        println!("🟢  Start test_device");

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        println!("🟢 1. Global Initialize...");
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
        // Device _la
        println!("🟢 4. Testing Device initialization...");

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 2);

        let (device_pubkey, bump_seed) =
            get_device_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "la".to_string(),
                device_type: DeviceType::Switch,
                location_pk: location_pubkey,
                exchange_pk: exchange_pubkey,
                public_ip: [10, 0, 0, 1],
                dz_prefixes: vec![([10, 1, 0, 0], 23)],
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

        let device_la = get_account_data(&mut banks_client, device_pubkey)
            .await
            .expect("Unable to get Account")
            .get_device();
        assert_eq!(device_la.account_type, AccountType::Device);
        assert_eq!(device_la.code, "la".to_string());
        assert_eq!(device_la.status, DeviceStatus::Pending);

        println!("✅ Device initialized successfully",);
        /*****************************************************************************************************************************************************/
        println!("🟢 5. Testing Activate Device...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ActivateDevice(device::activate::DeviceActivateArgs {
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
            .get_device();
        assert_eq!(device_la.account_type, AccountType::Device);
        assert_eq!(device_la.status, DeviceStatus::Activated);

        println!("✅ Device activated successfully");
        /***********************************************************************************************************************************/
        // Device _la
        println!("🟢 6. Testing User creation...");
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 3);

        let (user_pubkey, bump_seed) =
            get_user_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateUser(UserCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                client_ip: [100, 0, 0, 1],
                user_type: UserType::IBRL,
                device_pk: device_pubkey,
                cyoa_type: UserCYOA::GREOverDIA,
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
        assert_eq!(user.client_ip, [100, 0, 0, 1]);
        assert_eq!(user.device_pk, device_pubkey);
        assert_eq!(user.status, UserStatus::Pending);

        println!("✅ User created successfully",);
        /***********************************************************************************************************************************/
        println!("🟢 7. Testing User activation...");

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                index: user.index,
                bump_seed: user.bump_seed,
                tunnel_id: 500,
                tunnel_net: ([10, 1, 2, 3], 21),
                dz_ip: [200, 0, 0, 1],
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
        assert_eq!(user.tunnel_id, 500);
        assert_eq!(user.tunnel_net, ([10, 1, 2, 3], 21));
        assert_eq!(user.dz_ip, [200, 0, 0, 1]);
        assert_eq!(user.status, UserStatus::Activated);

        println!("✅ User created successfully",);
        /*****************************************************************************************************************************************************/
        println!("🟢 8. Testing user suspend...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SuspendUser(UserSuspendArgs {
                index: user.index,
                bump_seed: user.bump_seed,
            }),
            vec![AccountMeta::new(user_pubkey, false)],
            &payer,
        )
        .await;

        let user = get_account_data(&mut banks_client, user_pubkey)
            .await
            .expect("Unable to get Account")
            .get_user();
        assert_eq!(user.account_type, AccountType::User);
        assert_eq!(user.status, UserStatus::Suspended);

        println!("✅ User suspended");
        /*****************************************************************************************************************************************************/
        println!("🟢 9. Testing User resumed...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ResumeUser(UserResumeArgs {
                index: user.index,
                bump_seed: user.bump_seed,
            }),
            vec![AccountMeta::new(user_pubkey, false)],
            &payer,
        )
        .await;

        let user = get_account_data(&mut banks_client, user_pubkey)
            .await
            .expect("Unable to get Account")
            .get_user();
        assert_eq!(user.account_type, AccountType::User);
        assert_eq!(user.status, UserStatus::Activated);

        println!("✅ User resumed");
        /*****************************************************************************************************************************************************/
        println!("🟢 10. Testing User update...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                index: user.index,
                bump_seed: user.bump_seed,
                client_ip: Some([10, 2, 3, 4]),
                user_type: Some(UserType::IBRL),
                cyoa_type: Some(UserCYOA::GREOverPrivatePeering),
                dz_ip: Some([200, 0, 0, 4]),
                tunnel_id: Some(501),
                tunnel_net: Some(([10, 1, 2, 4], 22)),
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
            .expect("Unable t get Account")
            .get_user();
        assert_eq!(user.account_type, AccountType::User);
        assert_eq!(user.client_ip, [10, 2, 3, 4]);
        assert_eq!(user.cyoa_type, UserCYOA::GREOverPrivatePeering);
        assert_eq!(user.status, UserStatus::Activated);

        println!("✅ User updated");
        /*****************************************************************************************************************************************************/
        println!("🟢 11. Testing User deletion...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
                index: user.index,
                bump_seed: user.bump_seed,
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
            .expect("Unable t get Account")
            .get_user();
        assert_eq!(user.account_type, AccountType::User);
        assert_eq!(user.client_ip, [10, 2, 3, 4]);
        assert_eq!(user.cyoa_type, UserCYOA::GREOverPrivatePeering);
        assert_eq!(user.status, UserStatus::Deleting);

        println!("✅ Link deleting");

        /*****************************************************************************************************************************************************/
        println!("🟢 12. Testing User deactivation...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {
                index: user.index,
                bump_seed: user.bump_seed,
            }),
            vec![
                AccountMeta::new(user_pubkey, false),
                AccountMeta::new(user.owner, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let user = get_account_data(&mut banks_client, user_pubkey).await;
        assert_eq!(user, None);

        println!("✅ Link deleted successfully");

        println!("🟢🟢🟢  End test_device  🟢🟢🟢");
    }
}
