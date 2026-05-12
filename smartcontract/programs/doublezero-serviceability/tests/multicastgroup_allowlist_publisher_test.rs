use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
        globalstate::setauthority::SetAuthorityArgs,
        multicastgroup::{
            allowlist::publisher::{
                add::AddMulticastGroupPubAllowlistArgs,
                remove::RemoveMulticastGroupPubAllowlistArgs,
            },
            create::MulticastGroupCreateArgs,
        },
    },
    resource::ResourceType,
    state::accesspass::AccessPassType,
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, signature::Keypair, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_multicast_publisher_allowlist_sentinel_authority() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let client_ip = [100, 0, 0, 2].into();
    let user_payer = payer.pubkey();

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // 1. Initialize global state
    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

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
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
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

/// AccessPass with allow_multiple_ip=true (dynamic PDA at 0.0.0.0) can be added/removed from publisher allowlist.
#[tokio::test]
async fn test_multicast_publisher_allowlist_allow_multiple_ip() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let user_payer = payer.pubkey();
    let dynamic_ip: std::net::Ipv4Addr = [0, 0, 0, 0].into();

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

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
            code: "amip-pub".to_string(),
            max_bandwidth: 1_000_000_000,
            owner: payer.pubkey(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    // Create AccessPass at dynamic PDA (0.0.0.0) with allow_multiple_ip=true
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &dynamic_ip, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: dynamic_ip,
            last_access_epoch: 100,
            allow_multiple_ip: true,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    // Add with client_ip=0.0.0.0 and dynamic PDA — should succeed
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: dynamic_ip,
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
    assert!(
        res.is_ok(),
        "allow_multiple_ip AccessPass should be addable to publisher allowlist"
    );

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert!(accesspass
        .mgroup_pub_allowlist
        .contains(&multicastgroup_pubkey));

    // Remove with client_ip=0.0.0.0 and dynamic PDA — should succeed
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
            RemoveMulticastGroupPubAllowlistArgs {
                client_ip: dynamic_ip,
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
    assert!(
        res.is_ok(),
        "allow_multiple_ip AccessPass should be removable from publisher allowlist"
    );

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(accesspass.mgroup_pub_allowlist.len(), 0);
}

/// A real client_ip in instruction args still works when the AccessPass has allow_multiple_ip=true
/// and lives at the dynamic PDA. This is the bug that was fixed.
#[tokio::test]
async fn test_multicast_publisher_allowlist_allow_multiple_ip_real_ip_in_args() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let user_payer = payer.pubkey();
    let dynamic_ip: std::net::Ipv4Addr = [0, 0, 0, 0].into();
    let real_ip: std::net::Ipv4Addr = [98, 46, 188, 245].into();

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

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
            code: "amip-pub-real-ip".to_string(),
            max_bandwidth: 1_000_000_000,
            owner: payer.pubkey(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    // Create AccessPass at dynamic PDA (0.0.0.0) with allow_multiple_ip=true
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &dynamic_ip, &user_payer);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: dynamic_ip,
            last_access_epoch: 100,
            allow_multiple_ip: true,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    // Add with a real IP in args but the dynamic PDA as account — should succeed
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: real_ip,
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
    assert!(
        res.is_ok(),
        "allow_multiple_ip AccessPass should accept real IP in args while using dynamic PDA"
    );

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert!(accesspass
        .mgroup_pub_allowlist
        .contains(&multicastgroup_pubkey));

    // Remove with a real IP in args but the dynamic PDA as account — should succeed
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
            RemoveMulticastGroupPubAllowlistArgs {
                client_ip: real_ip,
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
    assert!(
        res.is_ok(),
        "allow_multiple_ip AccessPass should accept real IP for remove with dynamic PDA"
    );

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(accesspass.mgroup_pub_allowlist.len(), 0);
}

/// Passing the wrong AccessPass PDA (one derived from a different IP) is rejected.
#[tokio::test]
async fn test_multicast_publisher_allowlist_wrong_pda_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let user_payer = payer.pubkey();
    let ip_a: std::net::Ipv4Addr = [10, 0, 2, 3].into();
    let ip_b: std::net::Ipv4Addr = [10, 0, 2, 4].into();

    let (_program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

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
            code: "pub-wrong-pda".to_string(),
            max_bandwidth: 1_000_000_000,
            owner: payer.pubkey(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    // Create AccessPass A at PDA(ip_a, user_payer)
    let (accesspass_a, _) = get_accesspass_pda(&program_id, &ip_a, &user_payer);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: ip_a,
            last_access_epoch: 100,
            allow_multiple_ip: false,
        }),
        vec![
            AccountMeta::new(accesspass_a, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    // Create AccessPass B at PDA(ip_b, user_payer)
    let (accesspass_b, _) = get_accesspass_pda(&program_id, &ip_b, &user_payer);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: ip_b,
            last_access_epoch: 100,
            allow_multiple_ip: false,
        }),
        vec![
            AccountMeta::new(accesspass_b, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    // Attempt to add with client_ip=ip_a in args but pass accesspass_b account — should fail
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: ip_a,
            user_payer,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_b, false), // wrong PDA
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    assert!(
        res.is_err(),
        "AccessPass PDA mismatch should be rejected for publisher allowlist"
    );
}
