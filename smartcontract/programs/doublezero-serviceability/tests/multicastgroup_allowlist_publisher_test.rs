use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
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
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signer::Signer};

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
            tenant: Pubkey::default(),
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
