use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{get_feed_pda, get_globalstate_pda, get_program_config_pda},
    processors::feed::{create::FeedCreateArgs, delete::FeedDeleteArgs, update::FeedUpdateArgs},
    state::accounttype::AccountType,
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    program_error::ProgramError,
    pubkey::Pubkey,
    signature::Signer,
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

/// The `Custom` code the program returns for `err`, derived from the enum rather than inlined
/// so a renumbering of the error variants can never silently pass a hard-coded literal.
fn custom_code(err: DoubleZeroError) -> u32 {
    match ProgramError::from(err) {
        ProgramError::Custom(code) => code,
        other => panic!("expected Custom, got {other:?}"),
    }
}

/// Run `instruction` and return the structured `TransactionError` on failure so a negative test
/// can match the exact `InstructionError::Custom(code)` at instruction index 0.
///
/// NOTE: this intentionally does not return program logs. With the native `processor!` harness the
/// guest program's `msg!` output is not surfaced to BanksClient, so the structured error code at
/// instruction index 0 is the reliable signal for which check fired.
async fn try_execute_and_get_error(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    instruction: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &solana_sdk::signature::Keypair,
) -> Result<(), TransactionError> {
    let recent_blockhash = wait_for_new_blockhash(banks_client).await;
    let mut transaction = create_transaction(program_id, &instruction, &accounts, payer);
    transaction.try_sign(&[payer], recent_blockhash).unwrap();

    banks_client
        .process_transaction_with_metadata(transaction)
        .await
        .expect("banks client failed")
        .result
}

/// Assert `result` failed at instruction index 0 with `Custom(expected)`.
fn assert_custom_at_ix0(result: &Result<(), TransactionError>, expected: u32) {
    match result {
        Err(TransactionError::InstructionError(0, InstructionError::Custom(code))) => {
            assert_eq!(*code, expected, "unexpected custom error code");
        }
        other => panic!("expected Custom({expected}) at instruction 0, got {other:?}"),
    }
}

/// Initialize global state so the default payer is on the foundation allowlist and thus authorized
/// for feed catalog instructions.
async fn init_globalstate(
    banks_client: &mut solana_program_test::BanksClient,
    program_id: Pubkey,
    payer: &solana_sdk::signature::Keypair,
    recent_blockhash: solana_program::hash::Hash,
) -> Pubkey {
    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        payer,
    )
    .await;

    globalstate_pubkey
}

#[tokio::test]
async fn test_feed_create_get_update_delete() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "shreds", &exchange);

    let g1 = Pubkey::new_unique();
    let g2 = Pubkey::new_unique();
    let groups = vec![g1, g2];

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "shreds".to_string(),
            name: "Shreds".to_string(),
            exchange,
            groups: groups.clone(),
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let feed = get_account_data(&mut banks_client, feed_pubkey)
        .await
        .expect("Unable to get Feed")
        .get_feed()
        .unwrap();
    assert_eq!(feed.account_type, AccountType::Feed);
    assert_eq!(feed.code, "shreds".to_string());
    assert_eq!(feed.name, "Shreds".to_string());
    assert_eq!(feed.owner, payer.pubkey());
    assert_eq!(feed.exchange, exchange);
    assert_eq!(feed.groups, groups);

    // Update name and groups (exchange is immutable — it's a seed).
    let new_groups = vec![Pubkey::new_unique()];
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateFeed(FeedUpdateArgs {
            name: Some("Shreds v2".to_string()),
            groups: Some(new_groups.clone()),
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let feed = get_account_data(&mut banks_client, feed_pubkey)
        .await
        .expect("Unable to get Feed")
        .get_feed()
        .unwrap();
    assert_eq!(feed.name, "Shreds v2".to_string());
    assert_eq!(feed.exchange, exchange);
    assert_eq!(feed.groups, new_groups);

    // Delete.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteFeed(FeedDeleteArgs {}),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert_eq!(get_account_data(&mut banks_client, feed_pubkey).await, None);
}

#[tokio::test]
async fn test_feed_same_code_different_exchange_allowed() {
    // One code is one SKU offered in many metros; each (code, exchange) is a distinct feed account.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let tokyo = Pubkey::new_unique();
    let london = Pubkey::new_unique();
    let (tokyo_feed, _) = get_feed_pda(&program_id, "hyperliquid", &tokyo);
    let (london_feed, _) = get_feed_pda(&program_id, "hyperliquid", &london);
    assert_ne!(tokyo_feed, london_feed);

    for (exchange, feed_pubkey) in [(tokyo, tokyo_feed), (london, london_feed)] {
        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
                code: "hyperliquid".to_string(),
                name: "Hyperliquid".to_string(),
                exchange,
                groups: vec![Pubkey::new_unique()],
            }),
            vec![
                AccountMeta::new(feed_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
            &payer,
        )
        .await;
    }

    for (exchange, feed_pubkey) in [(tokyo, tokyo_feed), (london, london_feed)] {
        let feed = get_account_data(&mut banks_client, feed_pubkey)
            .await
            .expect("Unable to get Feed")
            .get_feed()
            .unwrap();
        assert_eq!(feed.code, "hyperliquid");
        assert_eq!(feed.exchange, exchange);
    }
}

#[tokio::test]
async fn test_feed_create_duplicate_rejected() {
    // The same (code, exchange) derives the same PDA, so a second create hits the already-created
    // account.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "dupe", &exchange);

    let create = |name: &str| {
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "dupe".to_string(),
            name: name.to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
        })
    };

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        create("First"),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        create("Second"),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("AccountAlreadyInitialized"),
        "Expected AccountAlreadyInitialized, got: {error_string}"
    );
}

#[tokio::test]
async fn test_feed_create_empty_groups_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "empty", &exchange);

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "empty".to_string(),
            name: "Empty".to_string(),
            exchange,
            groups: vec![],
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

#[tokio::test]
async fn test_feed_create_duplicate_group_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "dupgrp", &exchange);

    let group = Pubkey::new_unique();
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "dupgrp".to_string(),
            name: "Dup group".to_string(),
            exchange,
            groups: vec![group, group],
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

#[tokio::test]
async fn test_feed_create_default_exchange_rejected() {
    // Every feed is scoped to a real metro; the default pubkey is not a valid exchange.
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (feed_pubkey, _) = get_feed_pda(&program_id, "nodef", &Pubkey::default());

    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "nodef".to_string(),
            name: "No default".to_string(),
            exchange: Pubkey::default(),
            groups: vec![Pubkey::new_unique()],
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::InvalidArgument));
}

#[tokio::test]
async fn test_feed_create_unauthorized_caller_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let exchange = Pubkey::new_unique();
    let (feed_pubkey, _) = get_feed_pda(&program_id, "unauth", &exchange);

    // test_payer() is funded but not on the foundation allowlist, so it is not authorized.
    let unauthorized = test_payer();
    let result = try_execute_and_get_error(
        &mut banks_client,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "unauth".to_string(),
            name: "Unauthorized".to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &unauthorized,
    )
    .await;

    assert_custom_at_ix0(&result, custom_code(DoubleZeroError::NotAllowed));
}
