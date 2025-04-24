#[cfg(test)]
mod device_test {
    use crate::entrypoint::*;
    use crate::instructions::*;
    use crate::pda::*;
    use crate::processors::allowlist::device::add::AddDeviceAllowlistGlobalConfigArgs;
    use crate::processors::allowlist::device::remove::RemoveDeviceAllowlistGlobalConfigArgs;
    use crate::state::accounttype::AccountType;
    use crate::tests::test::*;
    use solana_program_test::*;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

    #[tokio::test]
    async fn device_allowlist_test() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "double_zero_sla_program",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        /***********************************************************************************************************************************/
        println!("🟢  Start device_allowlist_test");

        let user1 = Pubkey::new_unique();
        let user2 = Pubkey::new_unique();

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

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

        /*****************************************************************************************************************************************************/
        println!("🟢 2. Add user1 to device allowlist...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddDeviceAllowlistGlobalConfig(
                AddDeviceAllowlistGlobalConfigArgs { pubkey: user1 },
            ),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            &payer,
        )
        .await;

        let state = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .expect("Unable to get Account")
            .get_global_state();

        assert_eq!(state.account_type, AccountType::GlobalState);
        assert_eq!(state.device_allowlist.len(), 2);
        assert!(state.device_allowlist.contains(&user1));
        
        println!("✅ Allowlist is correct");
        /*****************************************************************************************************************************************************/
        println!("🟢 3. Add user2 to device allowlist...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddDeviceAllowlistGlobalConfig(
                AddDeviceAllowlistGlobalConfigArgs { pubkey: user2 },
            ),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            &payer,
        )
        .await;

        let state = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .expect("Unable to get Account")
            .get_global_state();

        assert_eq!(state.account_type, AccountType::GlobalState);
        assert_eq!(state.device_allowlist.len(), 3);
        assert!(state.device_allowlist.contains(&user1));
        assert!(state.device_allowlist.contains(&user2));

        println!("✅ Allowlist is correct");
        /*****************************************************************************************************************************************************/
        println!("🟢 4. Remove user1 to device allowlist...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::RemoveDeviceAllowlistGlobalConfig(
                RemoveDeviceAllowlistGlobalConfigArgs { pubkey: user1 },
            ),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            &payer,
        )
        .await;

        let state = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .expect("Unable to get Account")
            .get_global_state();

        assert_eq!(state.account_type, AccountType::GlobalState);
        assert_eq!(state.device_allowlist.len(), 2);
        assert!(!state.device_allowlist.contains(&user1));
        assert!(state.device_allowlist.contains(&user2));

        println!("✅ Allowlist is correct");
        /*****************************************************************************************************************************************************/
        println!("🟢 5. Remove user2 to device allowlist...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::RemoveDeviceAllowlistGlobalConfig(
                RemoveDeviceAllowlistGlobalConfigArgs { pubkey: user2 },
            ),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            &payer,
        )
        .await;

        let state = get_account_data(&mut banks_client, globalstate_pubkey)
            .await
            .expect("Unable to get Account")
            .get_global_state();

        assert_eq!(state.account_type, AccountType::GlobalState);
        assert_eq!(state.device_allowlist.len(), 1);
        assert!(!state.device_allowlist.contains(&user1));
        assert!(!state.device_allowlist.contains(&user2));

        println!("✅ Allowlist is correct");
        /*****************************************************************************************************************************************************/
        println!("🟢🟢🟢  End test_device  🟢🟢🟢");
    }
}
