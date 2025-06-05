#[cfg(test)]
mod location_test {
    use crate::{
        entrypoint::*,
        instructions::*,
        pda::*,
        processors::location::{create::*, delete::*, resume::*, suspend::*, update::*},
        state::{accounttype::AccountType, location::*},
        tests::test::*,
    };
    use solana_program_test::*;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

    #[tokio::test]
    async fn test_location() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "doublezero_sla_program",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        /***********************************************************************************************************************************/
        println!("🟢  Start test_location");
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::InitGlobalState(),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            &payer,
        )
        .await;

        /***********************************************************************************************************************************/
        // Location _la

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        println!("Testing Location initialization...");
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 0);

        let (location_pubkey, bump_seed) =
            get_location_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
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

        let location_la = get_account_data(&mut banks_client, location_pubkey)
            .await
            .expect("Unable to get Account")
            .get_location();
        assert_eq!(location_la.account_type, AccountType::Location);
        assert_eq!(location_la.code, "la".to_string());
        assert_eq!(location_la.status, LocationStatus::Activated);

        println!("✅ Location initialized successfully",);
        /*****************************************************************************************************************************************************/
        println!("Testing Location suspend...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SuspendLocation(LocationSuspendArgs {
                index: location_la.index,
                bump_seed: location_la.bump_seed,
            }),
            vec![
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let location_la = get_account_data(&mut banks_client, location_pubkey)
            .await
            .expect("Unable to get Account")
            .get_location();
        assert_eq!(location_la.account_type, AccountType::Location);
        assert_eq!(location_la.status, LocationStatus::Suspended);

        println!("✅ Location suspended");
        /*****************************************************************************************************************************************************/
        println!("Testing Location resumed...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ResumeLocation(LocationResumeArgs {
                index: location_la.index,
                bump_seed: location_la.bump_seed,
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
        assert_eq!(location.status, LocationStatus::Activated);

        println!("✅ Location resumed");
        /*****************************************************************************************************************************************************/
        println!("Testing Location update...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::UpdateLocation(LocationUpdateArgs {
                index: location.index,
                bump_seed: location.bump_seed,
                code: Some("la2".to_string()),
                name: Some("Los Angeles - Los Angeles".to_string()),
                country: Some("CA".to_string()),
                lat: Some(3.433),
                lng: Some(23.223),
                loc_id: Some(1),
            }),
            vec![
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let location_la = get_account_data(&mut banks_client, location_pubkey)
            .await
            .expect("Unable to get Account")
            .get_location();
        assert_eq!(location_la.account_type, AccountType::Location);
        assert_eq!(location_la.code, "la2".to_string());
        assert_eq!(location_la.name, "Los Angeles - Los Angeles".to_string());
        assert_eq!(location_la.status, LocationStatus::Activated);

        println!("✅ Location updated");
        /*****************************************************************************************************************************************************/
        println!("Testing Location deletion...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs {
                index: location_la.index,
                bump_seed: location_la.bump_seed,
            }),
            vec![
                AccountMeta::new(location_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let location_la = get_account_data(&mut banks_client, location_pubkey).await;
        assert_eq!(location_la, None);

        println!("✅ Location deleted successfully");
        println!("🟢  End test_location");
    }
}
