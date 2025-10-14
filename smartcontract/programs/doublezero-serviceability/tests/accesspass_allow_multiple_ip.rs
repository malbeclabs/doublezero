use doublezero_serviceability::{
    entrypoint::*,
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::update::DeviceUpdateArgs,
        user::{activate::*, create::*, delete::*, resume::*, suspend::*, update::*},
        *,
    },
    state::{
        accesspass::{AccessPassStatus, AccessPassType},
        accounttype::AccountType,
        contributor::ContributorStatus,
        device::*,
        user::{UserCYOA, UserStatus, UserType},
    },
};
use globalconfig::set::SetGlobalConfigArgs;
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signer::Signer};
use std::net::Ipv4Addr;
use user::closeaccount::UserCloseAccountArgs;

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_accesspass_allow_multiple_ip() {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_user");

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
    println!("🟢 2. Create Location...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);

    let (location_pubkey, _) = get_location_pda(&program_id, globalstate_account.account_index + 1);

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
    println!("🟢 3. Create Exchange...");

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 1);

    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            lat: 1.234,
            lng: 4.567,
            bgp_community: 0,
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

    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "cont".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
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
    println!("🟢 4. Testing Device initialization...");

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 3);

    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
            code: "la".to_string(),
            device_type: DeviceType::Switch,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "100.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
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

    let device_la = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_la.account_type, AccountType::Device);
    assert_eq!(device_la.code, "la".to_string());
    assert_eq!(device_la.status, DeviceStatus::Pending);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(128),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device_la = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device_la.max_users, 128);

    println!("✅ Device initialized successfully",);
    /*****************************************************************************************************************************************************/
    println!("🟢 5. Testing Activate Device...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(device::activate::DeviceActivateArgs),
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
    assert_eq!(device_la.status, DeviceStatus::Activated);

    println!("✅ Device activated successfully");
    /***********************************************************************************************************************************/
    println!("🟢 6. Testing Access Pass creation...");

    let user_ip = [100, 0, 0, 1].into();
    let (accesspass_pubkey, _) =
        get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer.pubkey());

    println!("Testing AccessPass User1 initialization...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: Ipv4Addr::UNSPECIFIED,
            last_access_epoch: 9999,
            allow_multiple_ip: true,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Check account data
    let user1 = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get User")
        .get_accesspass()
        .unwrap();
    assert_eq!(user1.account_type, AccountType::AccessPass);
    assert_eq!(user1.status, AccessPassStatus::Requested);
    /***********************************************************************************************************************************/
    // Device _la
    println!("🟢 7. Testing User creation...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 4);

    let (user_pubkey, _) = get_user_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.client_ip.to_string(), "100.0.0.1");
    assert_eq!(user.device_pk, device_pubkey);
    assert_eq!(user.status, UserStatus::Pending);

    println!("✅ User created successfully",);
    /***********************************************************************************************************************************/
    println!("🟢 8. Testing User activation...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(UserActivateArgs {
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            dz_ip: [200, 0, 0, 1].into(),
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.tunnel_id, 500);
    assert_eq!(user.tunnel_net.to_string(), "169.254.0.0/25");
    assert_eq!(user.dz_ip.to_string(), "200.0.0.1");
    assert_eq!(user.status, UserStatus::Activated);

    println!("✅ User created successfully",);
    /*****************************************************************************************************************************************************/
    println!("🟢 9. Testing user suspend...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendUser(UserSuspendArgs {}),
        vec![AccountMeta::new(user_pubkey, false)],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.status, UserStatus::Suspended);

    println!("✅ User suspended");
    /*****************************************************************************************************************************************************/
    println!("🟢 10. Testing User resumed...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ResumeUser(UserResumeArgs {}),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.status, UserStatus::Activated);

    println!("✅ User resumed");
    /*****************************************************************************************************************************************************/
    println!("🟢 11a. Testing User update...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
            user_type: Some(UserType::IBRL),
            cyoa_type: Some(UserCYOA::GREOverPrivatePeering),
            dz_ip: Some([200, 0, 0, 4].into()),
            tunnel_id: Some(501),
            tunnel_net: Some("169.254.0.2/25".parse().unwrap()),
            validator_pubkey: None,
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
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.client_ip.to_string(), "100.0.0.1");
    assert_eq!(user.cyoa_type, UserCYOA::GREOverPrivatePeering);
    assert_eq!(user.status, UserStatus::Activated);

    println!("✅ User updated");
    /*****************************************************************************************************************************************************/
    println!("🟢 11b. Testing User update (regression test: unspecified dz_ip should not clear the dz_ip)...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
            user_type: Some(UserType::IBRL),
            cyoa_type: Some(UserCYOA::GREOverPrivatePeering),
            dz_ip: None,
            tunnel_id: Some(505),
            tunnel_net: Some("169.254.0.2/25".parse().unwrap()),
            validator_pubkey: None,
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
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.client_ip.to_string(), "100.0.0.1");
    assert_eq!(user.cyoa_type, UserCYOA::GREOverPrivatePeering);
    assert_eq!(user.status, UserStatus::Activated);
    assert_eq!(user.dz_ip.to_string(), "200.0.0.4");

    println!("✅ User updated");
    /*****************************************************************************************************************************************************/
    println!("🟢 12. Testing User deletion...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {}),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("Unable to get Account")
        .get_user()
        .unwrap();
    assert_eq!(user.account_type, AccountType::User);
    assert_eq!(user.client_ip.to_string(), "100.0.0.1");
    assert_eq!(user.cyoa_type, UserCYOA::GREOverPrivatePeering);
    assert_eq!(user.status, UserStatus::Deleting);

    println!("✅ Link deleting");

    /*****************************************************************************************************************************************************/
    println!("🟢 13. Testing User deactivation...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountUser(UserCloseAccountArgs {}),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(user.owner, false),
            AccountMeta::new(user.device_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let user = get_account_data(&mut banks_client, user_pubkey).await;
    assert_eq!(user, None);

    println!("✅ Link deleted successfully");

    println!("🟢🟢🟢  End test_user  🟢🟢🟢");
}
