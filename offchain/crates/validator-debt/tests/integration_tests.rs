// TODO
// #[ignore = "need local validator"]
// #[tokio::test]
// async fn test_initialize_distribution_flow() -> Result<()> {
//     let keypair = try_load_keypair(None).unwrap();
//     let commitment_config = CommitmentConfig::confirmed();
//     let ledger_rpc_client = RpcClient::new_with_commitment(ledger_rpc(), commitment_config);
//
//     let solana_rpc_client = RpcClient::new_with_commitment(solana_rpc(), commitment_config);
//     let vote_account_config = RpcGetVoteAccountsConfig {
//         vote_pubkey: None,
//         commitment: CommitmentConfig::finalized().into(),
//         keep_unstaked_delinquents: None,
//         delinquent_slot_distance: None,
//     };
//
//     let rpc_block_config = RpcBlockConfig {
//         encoding: Some(UiTransactionEncoding::Base58),
//         transaction_details: Some(TransactionDetails::Signatures),
//         rewards: Some(true),
//         commitment: None,
//         max_supported_transaction_version: Some(0),
//     };
//     let fpc = SolanaDebtCalculator::new(
//         ledger_rpc_client,
//         solana_rpc_client,
//         rpc_block_config,
//         vote_account_config,
//     );
//     let dz_epoch_info = fpc.ledger_rpc_client.get_epoch_info().await?;
//     let transaction = Transaction::new(keypair, true, false);
//     initialize_distribution(&fpc, transaction, dz_epoch_info.epoch).await?;
//
//     Ok(())
// }

// TODO
// #[ignore = "needs local validator"]
// #[tokio::test]
// async fn test_write_to_read_from_chain() -> anyhow::Result<()> {
//     let keypair = try_load_keypair(None).unwrap();
//     let k = keypair.pubkey();
//     let validator_id = "devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj";
//     let commitment_config = CommitmentConfig::processed();
//     let ledger_rpc_client =
//         RpcClient::new_with_commitment("http://localhost:8899".to_string(), commitment_config);
//
//     let solana_rpc_client =
//         RpcClient::new_with_commitment("http://localhost:8899".to_string(), commitment_config);
//     let vote_account_config = RpcGetVoteAccountsConfig {
//         vote_pubkey: Some(validator_id.to_string()),
//         commitment: CommitmentConfig::finalized().into(),
//         keep_unstaked_delinquents: None,
//         delinquent_slot_distance: None,
//     };
//
//     let rpc_block_config = RpcBlockConfig {
//         encoding: Some(UiTransactionEncoding::Base58),
//         transaction_details: Some(TransactionDetails::None),
//         rewards: Some(true),
//         commitment: None,
//         max_supported_transaction_version: Some(0),
//     };
//     let fpc = SolanaDebtCalculator::new(
//         ledger_rpc_client,
//         solana_rpc_client,
//         rpc_block_config,
//         vote_account_config,
//     );
//     let solana_rpc_client = fpc.solana_rpc_client;
//
//     let tx_sig = solana_rpc_client
//         .request_airdrop(&k, 1_000_000_000)
//         .await
//         .unwrap();
//
//     while !solana_rpc_client
//         .confirm_transaction_with_commitment(&tx_sig, commitment_config)
//         .await
//         .unwrap()
//         .value
//     {
//         tokio::time::sleep(Duration::from_millis(400)).await;
//     }
//
//     // Make sure airdrop went through.
//     while solana_rpc_client
//         .get_balance_with_commitment(&k, commitment_config)
//         .await
//         .unwrap()
//         .value
//         == 0
//     {
//         // Airdrop doesn't get processed after a slot unfortunately.
//         tokio::time::sleep(Duration::from_secs(2)).await;
//     }
//
//     let transaction = Transaction::new(keypair, false, false);
//
//     let new_transaction = transaction
//         .initialize_distribution(&solana_rpc_client, 0, 0)
//         .await?;
//
//     let _sent_transaction = transaction
//         .send_or_simulate_transaction(&solana_rpc_client, &new_transaction)
//         .await?;
//
//     let debt = RevenueDistributionInstructionData::ConfigureDistributionDebt {
//         total_validators: 5,
//         total_debt: 100_000,
//         merkle_root: Hash::from_str("7biGoeW59qKyVEqL2iWAm6H4hhRCExk6LxbgGrpXptci").unwrap(),
//     };
//
//     let dz_epoch = 0;
//     let t = transaction
//         .submit_distribution(&solana_rpc_client, dz_epoch, debt)
//         .await?;
//
//     let _tr = transaction
//         .send_or_simulate_transaction(&solana_rpc_client, &t)
//         .await?;
//
//     let _rt = transaction.read_distribution(0, &solana_rpc_client).await?;
//
//     Ok(())
// }
