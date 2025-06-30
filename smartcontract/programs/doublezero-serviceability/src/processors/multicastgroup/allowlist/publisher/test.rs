#[cfg(test)]
mod device_test {
    use crate::{
        entrypoint::*,
        instructions::*,
        pda::*,
        processors::multicastgroup::{
            activate::MulticastGroupActivateArgs,
            allowlist::publisher::{
                add::AddMulticastGroupPubAllowlistArgs,
                remove::RemoveMulticastGroupPubAllowlistArgs,
            },
            create::MulticastGroupCreateArgs,
        },
        state::{accounttype::AccountType, multicastgroup::MulticastGroupStatus},
        tests::test::*,
    };
    use solana_program_test::*;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signer::Signer};

    #[tokio::test]
    async fn test_multicast_publisher_allowlist() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "doublezero_serviceability",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        /***********************************************************************************************************************************/
        println!("🟢 1. Global Initialization...");

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

        let (multicastgroup_pubkey, bump_seed) =
            get_multicastgroup_pda(&program_id, globalstate.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                index: globalstate.account_index + 1,
                bump_seed,
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
                multicast_ip: [223, 0, 0, 1].into(),
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
        assert_eq!(mgroup.multicast_ip.to_string(), "223.0.0.1");
        assert_eq!(mgroup.status, MulticastGroupStatus::Activated);

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 4. Add Allowlist ...");

        let (multicastgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);

        let allowlist_pubkey = Pubkey::new_unique();

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                AddMulticastGroupPubAllowlistArgs {
                    pubkey: allowlist_pubkey,
                },
            ),
            vec![AccountMeta::new(multicastgroup_pubkey, false)],
            &payer,
        )
        .await;

        let mgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup()
            .unwrap();

        assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
        assert_eq!(mgroup.pub_allowlist.len(), 1);
        assert!(mgroup.pub_allowlist.contains(&allowlist_pubkey));
        assert_eq!(mgroup.status, MulticastGroupStatus::Activated);

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢 5. Remove Allowlist ...");

        let (multicastgroup_pubkey, _) = get_multicastgroup_pda(&program_id, 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
                RemoveMulticastGroupPubAllowlistArgs {
                    pubkey: allowlist_pubkey,
                },
            ),
            vec![AccountMeta::new(multicastgroup_pubkey, false)],
            &payer,
        )
        .await;

        let mgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup()
            .unwrap();

        assert_eq!(mgroup.account_type, AccountType::MulticastGroup);
        assert_eq!(mgroup.pub_allowlist.len(), 0);
        assert_eq!(mgroup.status, MulticastGroupStatus::Activated);

        println!("✅");
        /*****************************************************************************************************************************************************/
        println!("🟢🟢🟢  End test  🟢🟢🟢");
    }
}
