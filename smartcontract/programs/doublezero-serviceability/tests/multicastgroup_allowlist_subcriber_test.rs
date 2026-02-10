use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
        multicastgroup::{
            activate::MulticastGroupActivateArgs,
            allowlist::subscriber::{
                add::AddMulticastGroupSubAllowlistArgs,
                remove::RemoveMulticastGroupSubAllowlistArgs,
            },
            create::MulticastGroupCreateArgs,
        },
    },
    state::{
        accesspass::AccessPassType, accounttype::AccountType,
        mgroup_allowlist_entry::MGroupAllowlistType, multicastgroup::MulticastGroupStatus,
    },
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_multicast_subscriber_allowlist() {
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
            multicast_ip: "224.254.0.1".parse().unwrap(),
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
    println!("🟢 4. Create AccessPass...");

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

    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    let (mgroup_al_entry_pk, _) = get_mgroup_allowlist_entry_pda(
        &program_id,
        &accesspass_pubkey,
        &multicastgroup_pubkey,
        MGroupAllowlistType::Subscriber as u8,
    );

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(AddMulticastGroupSubAllowlistArgs {
            client_ip,
            user_payer,
        }),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(mgroup_al_entry_pk, false),
        ],
        &payer,
    )
    .await;

    let al_entry = get_account_data(&mut banks_client, mgroup_al_entry_pk)
        .await
        .expect("Unable to get MGroupAllowlistEntry")
        .get_mgroup_allowlist_entry()
        .unwrap();
    assert_eq!(al_entry.account_type, AccountType::MGroupAllowlistEntry);
    assert_eq!(al_entry.allowlist_type, MGroupAllowlistType::Subscriber);

    println!("✅");
    /*****************************************************************************************************************************************************/
    println!("🟢 6. Remove Allowlist ...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveMulticastGroupSubAllowlist(
            RemoveMulticastGroupSubAllowlistArgs {
                client_ip,
                user_payer,
            },
        ),
        vec![
            AccountMeta::new(multicastgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(mgroup_al_entry_pk, false),
        ],
        &payer,
    )
    .await;

    // PDA should be closed after removal
    assert!(
        get_account_data(&mut banks_client, mgroup_al_entry_pk)
            .await
            .is_none(),
        "MGroupAllowlistEntry PDA should be closed after removal"
    );

    println!("✅");
    /*****************************************************************************************************************************************************/
    println!("🟢🟢🟢  End test  🟢🟢🟢");
}
