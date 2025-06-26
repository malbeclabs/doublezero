#[cfg(test)]
mod multicastgroup_test {
    use crate::{
        entrypoint::*,
        instructions::*,
        pda::*,
        processors::multicastgroup::{
            activate::MulticastGroupActivateArgs, create::*,
            deactivate::MulticastGroupDeactivateArgs, delete::*, reactivate::*, suspend::*,
            update::*,
        },
        state::{accounttype::AccountType, multicastgroup::*},
        tests::test::*,
    };
    use solana_program_test::*;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

    #[tokio::test]
    async fn test_multicastgroup() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "doublezero_serviceability",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        /***********************************************************************************************************************************/
        println!("🟢  Start test_multicastgroup");

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

        /***********************************************************************************************************************************/
        // MulticastGroup _la

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        println!("1. Testing MulticastGroup initialization...");
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 0);

        let (multicastgroup_pubkey, bump_seed) =
            get_multicastgroup_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                code: "la".to_string(),
                max_bandwidth: 1000,
                owner: Pubkey::new_unique(),
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup()
            .unwrap();
        assert_eq!(multicastgroup_la.account_type, AccountType::MulticastGroup);
        assert_eq!(multicastgroup_la.code, "la".to_string());
        assert_eq!(multicastgroup_la.multicast_ip, [0, 0, 0, 0]);
        assert_eq!(multicastgroup_la.max_bandwidth, 1000);
        assert_eq!(multicastgroup_la.status, MulticastGroupStatus::Pending);

        println!("✅ MulticastGroup initialized successfully",);

        /*****************************************************************************************************************************************************/
        println!("2. Testing MulticastGroup suspend...");

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ActivateMulticastGroup(MulticastGroupActivateArgs {
                index: globalstate_account.account_index + 1,
                bump_seed,
                multicast_ip: [224, 0, 0, 0],
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup()
            .unwrap();
        assert_eq!(multicastgroup_la.account_type, AccountType::MulticastGroup);
        assert_eq!(multicastgroup_la.code, "la".to_string());
        assert_eq!(multicastgroup_la.multicast_ip, [224, 0, 0, 0]);
        assert_eq!(multicastgroup_la.status, MulticastGroupStatus::Activated);

        println!("✅ MulticastGroup activate successfully",);
        /*****************************************************************************************************************************************************/
        println!("2. Testing MulticastGroup suspend...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SuspendMulticastGroup(MulticastGroupSuspendArgs {
                index: multicastgroup_la.index,
                bump_seed: multicastgroup_la.bump_seed,
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup()
            .unwrap();
        assert_eq!(multicastgroup_la.account_type, AccountType::MulticastGroup);
        assert_eq!(multicastgroup_la.status, MulticastGroupStatus::Suspended);

        println!("✅ MulticastGroup suspended");
        /*****************************************************************************************************************************************************/
        println!("3. Testing MulticastGroup reactivated...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ReactivateMulticastGroup(MulticastGroupReactivateArgs {
                index: multicastgroup_la.index,
                bump_seed: multicastgroup_la.bump_seed,
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let multicastgroup = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup()
            .unwrap();
        assert_eq!(multicastgroup.account_type, AccountType::MulticastGroup);
        assert_eq!(multicastgroup.status, MulticastGroupStatus::Activated);

        println!("✅ MulticastGroup reactivated");
        /*****************************************************************************************************************************************************/
        println!("4. Testing MulticastGroup update...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
                index: multicastgroup.index,
                bump_seed: multicastgroup.bump_seed,
                code: Some("la2".to_string()),
                multicast_ip: Some([239, 1, 1, 2]),
                max_bandwidth: Some(2000),
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup()
            .unwrap();
        assert_eq!(multicastgroup_la.account_type, AccountType::MulticastGroup);
        assert_eq!(multicastgroup_la.code, "la2".to_string());
        assert_eq!(multicastgroup_la.status, MulticastGroupStatus::Activated);

        println!("✅ MulticastGroup updated");
        /*****************************************************************************************************************************************************/
        println!("5. Testing MulticastGroup deletion...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::DeleteMulticastGroup(MulticastGroupDeleteArgs {
                index: multicastgroup_la.index,
                bump_seed: multicastgroup_la.bump_seed,
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey)
            .await
            .expect("Unable to get Account")
            .get_multicastgroup()
            .unwrap();
        assert_eq!(multicastgroup_la.account_type, AccountType::MulticastGroup);
        assert_eq!(multicastgroup_la.code, "la2".to_string());
        assert_eq!(multicastgroup_la.status, MulticastGroupStatus::Deleting);

        println!("✅ MulticastGroup deleted");
        /*****************************************************************************************************************************************************/
        println!("6. Testing MulticastGroup deactivation (final delete)...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::DeactivateMulticastGroup(MulticastGroupDeactivateArgs {
                index: multicastgroup_la.index,
                bump_seed: multicastgroup_la.bump_seed,
            }),
            vec![
                AccountMeta::new(multicastgroup_pubkey, false),
                AccountMeta::new(multicastgroup.owner, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let multicastgroup_la = get_account_data(&mut banks_client, multicastgroup_pubkey).await;
        assert_eq!(multicastgroup_la, None);

        println!("✅ MulticastGroup deleted successfully");
        println!("🟢  End test_multicastgroup");
    }
}
