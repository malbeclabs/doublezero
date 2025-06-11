#[cfg(test)]
mod exchange_test {
    use crate::{
        entrypoint::*,
        instructions::*,
        pda::*,
        processors::exchange::{create::*, delete::*, resume::*, suspend::*, update::*},
        state::{accounttype::AccountType, exchange::*},
        tests::test::*,
    };
    use solana_program_test::*;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

    #[tokio::test]
    async fn test_exchange() {
        let program_id = Pubkey::new_unique();
        let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
            "doublezero_serviceability",
            program_id,
            processor!(process_instruction),
        )
        .start()
        .await;

        /***********************************************************************************************************************************/
        println!("🟢  Start test_exchange");

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
        // Exchange _la

        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

        println!("Testing Exchange initialization...");
        let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
        assert_eq!(globalstate_account.account_index, 0);

        let (exchange_pubkey, bump_seed) =
            get_exchange_pda(&program_id, globalstate_account.account_index + 1);

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
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

        let exchange_la = get_account_data(&mut banks_client, exchange_pubkey)
            .await
            .expect("Unable to get Account")
            .get_exchange();
        assert_eq!(exchange_la.account_type, AccountType::Exchange);
        assert_eq!(exchange_la.code, "la".to_string());
        assert_eq!(exchange_la.status, ExchangeStatus::Activated);

        println!("✅ Exchange initialized successfully",);
        /*****************************************************************************************************************************************************/
        println!("Testing Exchange suspend...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::SuspendExchange(ExchangeSuspendArgs {
                index: exchange_la.index,
                bump_seed: exchange_la.bump_seed,
            }),
            vec![
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let exchange_la = get_account_data(&mut banks_client, exchange_pubkey)
            .await
            .expect("Unable to get Account")
            .get_exchange();
        assert_eq!(exchange_la.account_type, AccountType::Exchange);
        assert_eq!(exchange_la.status, ExchangeStatus::Suspended);

        println!("✅ Exchange suspended");
        /*****************************************************************************************************************************************************/
        println!("Testing Exchange resumed...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::ResumeExchange(ExchangeResumeArgs {
                index: exchange_la.index,
                bump_seed: exchange_la.bump_seed,
            }),
            vec![
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let exchange = get_account_data(&mut banks_client, exchange_pubkey)
            .await
            .expect("Unable to get Account")
            .get_exchange();
        assert_eq!(exchange.account_type, AccountType::Exchange);
        assert_eq!(exchange.status, ExchangeStatus::Activated);

        println!("✅ Exchange resumed");
        /*****************************************************************************************************************************************************/
        println!("Testing Exchange update...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
                index: exchange.index,
                bump_seed: exchange.bump_seed,
                code: Some("la2".to_string()),
                name: Some("Los Angeles - Los Angeles".to_string()),
                lat: Some(3.433),
                lng: Some(23.223),
                loc_id: Some(1),
            }),
            vec![
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let exchange_la = get_account_data(&mut banks_client, exchange_pubkey)
            .await
            .expect("Unable to get Account")
            .get_exchange();
        assert_eq!(exchange_la.account_type, AccountType::Exchange);
        assert_eq!(exchange_la.code, "la2".to_string());
        assert_eq!(exchange_la.name, "Los Angeles - Los Angeles".to_string());
        assert_eq!(exchange_la.status, ExchangeStatus::Activated);

        println!("✅ Exchange updated");
        /*****************************************************************************************************************************************************/
        println!("Testing Exchange deletion...");
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::DeleteExchange(ExchangeDeleteArgs {
                index: exchange_la.index,
                bump_seed: exchange_la.bump_seed,
            }),
            vec![
                AccountMeta::new(exchange_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;

        let exchange_la = get_account_data(&mut banks_client, exchange_pubkey).await;
        assert_eq!(exchange_la, None);

        println!("✅ Exchange deleted successfully");
        println!("🟢  End test_exchange");
    }
}
