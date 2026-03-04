use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
        globalstate::setauthority::SetAuthorityArgs,
        multicastgroup::{
            activate::MulticastGroupActivateArgs,
            allowlist::publisher::{
                add::AddMulticastGroupPubAllowlistArgs,
                remove::RemoveMulticastGroupPubAllowlistArgs,
            },
            create::MulticastGroupCreateArgs,
        },
    },
    state::{
        accesspass::AccessPassType, accounttype::AccountType, multicastgroup::MulticastGroupStatus,
    },
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, signature::Keypair, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_multicast_publisher_allowlist() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢 1. Global Initialization...");

    let user_payer = payer.pubkey();
    let client_ip = [100, 0, 0, 1].into();

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

    println!("✅");
    /*****************************************************************************************************************************************************/
    println!("🟢 2. Create MulticastGroup...");

    let globalstate = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "test".to_string(),
            max_bandwidth: 1_000_000_000,
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
        .get_multicastgroup()
        .unwrap();

    assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
    assert_eq!(mgroup.code, "test".to_string());
    assert_eq!(mgroup.status, MulticastGroupStatus::Pending);

    println!("✅");
    /*****************************************************************************************************************************************************/
    println!("🟢 3. Activate MulticastGroup...");

    let (multicastgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [224, 254, 0, 1].into(),
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
        .get_multicastgroup()
        .unwrap();

    assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
    assert_eq!(mgroup.multicast_ip.to_string(), "224.254.0.1");
    assert_eq!(mgroup.status, MulticastGroupStatus::Activated);

    println!("✅");
    /*****************************************************************************************************************************************************/
    println!("🟢 4. Set AccessPass...");

    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 100,
            allow_multiple_ip: false,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    /*****************************************************************************************************************************************************/
    println!("🟢 5. Add Allowlist ...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip,
            user_payer,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();

    assert_eq!(accesspass.account_type, AccountType::AccessPass);
    assert!(accesspass
        .mgroup_pub_allowlist
        .contains(&multicastgroup_pubkey));

    println!("✅");
    /*****************************************************************************************************************************************************/
    println!("🟢 6. Remove Allowlist ...");

    let (multicastgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
            RemoveMulticastGroupPubAllowlistArgs {
                client_ip,
                user_payer,
            },
        ),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();

    assert_eq!(accesspass.account_type, AccountType::AccessPass);
    assert_eq!(accesspass.mgroup_pub_allowlist.len(), 0);

    println!("✅");
    /*****************************************************************************************************************************************************/
    println!("🟢🟢🟢  End test  🟢🟢🟢");
}

#[tokio::test]
async fn test_multicast_publisher_allowlist_sentinel_authority() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let client_ip = [100, 0, 0, 2].into();
    let user_payer = payer.pubkey();

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // 1. Initialize global state
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

    // 2. Create a sentinel keypair and set it as sentinel authority
    let sentinel = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &sentinel.pubkey(),
        10_000_000_000,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            sentinel_authority_pk: Some(sentinel.pubkey()),
            ..Default::default()
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    // 3. Create and activate a multicast group (owned by payer, NOT sentinel)
    let globalstate = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    let (multicastgroup_pubkey, _) =
        get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "sentinel-test".to_string(),
            max_bandwidth: 1_000_000_000,
            owner: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
            multicast_ip: [224, 254, 0, 2].into(),
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // 4. Set access pass (requires foundation allowlist, so use payer)
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 100,
            allow_multiple_ip: false,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    // 5. Sentinel (non-owner) adds publisher allowlist entry — should succeed
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip,
            user_payer,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &sentinel,
    )
    .await;
    assert!(
        res.is_ok(),
        "Sentinel authority should be able to add publisher allowlist entry"
    );

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert!(accesspass
        .mgroup_pub_allowlist
        .contains(&multicastgroup_pubkey));

    // 6. Sentinel removes publisher allowlist entry — should succeed
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
            RemoveMulticastGroupPubAllowlistArgs {
                client_ip,
                user_payer,
            },
        ),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &sentinel,
    )
    .await;
    assert!(
        res.is_ok(),
        "Sentinel authority should be able to remove publisher allowlist entry"
    );

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(accesspass.mgroup_pub_allowlist.len(), 0);

    // 7. Unauthorized keypair should fail
    let unauthorized = Keypair::new();
    transfer(
        &mut banks_client,
        &payer,
        &unauthorized.pubkey(),
        10_000_000_000,
    )
    .await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip,
            user_payer,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &unauthorized,
    )
    .await;
    assert!(
        res.is_err(),
        "Unauthorized keypair should not be able to add publisher allowlist entry"
    );
}
