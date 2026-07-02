use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_feed_pda, get_globalstate_pda, get_program_config_pda},
    processors::feed::{create::FeedCreateArgs, delete::FeedDeleteArgs, update::FeedUpdateArgs},
    state::{accounttype::AccountType, feed::MetroGroups},
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signer};

mod test_helpers;
use test_helpers::*;

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

    let (feed_pubkey, _) = get_feed_pda(&program_id, "shreds");

    let exchange = Pubkey::new_unique();
    let g1 = Pubkey::new_unique();
    let g2 = Pubkey::new_unique();
    let metros = vec![MetroGroups {
        exchange,
        groups: vec![g1, g2],
    }];

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "shreds".to_string(),
            name: "Shreds".to_string(),
            metros: metros.clone(),
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
    assert_eq!(feed.reference_count, 0);
    assert_eq!(feed.owner, payer.pubkey());
    assert_eq!(feed.metros, metros);

    // Update name and metros.
    let new_exchange = Pubkey::new_unique();
    let new_metros = vec![MetroGroups {
        exchange: new_exchange,
        groups: vec![Pubkey::new_unique()],
    }];
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateFeed(FeedUpdateArgs {
            name: Some("Shreds v2".to_string()),
            metros: Some(new_metros.clone()),
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
    assert_eq!(feed.metros, new_metros);

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
async fn test_feed_create_duplicate_code_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (feed_pubkey, _) = get_feed_pda(&program_id, "dupe");

    let create = |name: &str| {
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "dupe".to_string(),
            name: name.to_string(),
            metros: vec![],
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
async fn test_feed_create_duplicate_exchange_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (feed_pubkey, _) = get_feed_pda(&program_id, "dup_ex");

    let exchange = Pubkey::new_unique();
    let metros = vec![
        MetroGroups {
            exchange,
            groups: vec![Pubkey::new_unique()],
        },
        MetroGroups {
            exchange,
            groups: vec![Pubkey::new_unique()],
        },
    ];

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "dup_ex".to_string(),
            name: "Dup".to_string(),
            metros,
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // InvalidArgument is Custom(65).
    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(65)"),
        "Expected InvalidArgument (Custom(65)), got: {error_string}"
    );
}

#[tokio::test]
async fn test_feed_update_duplicate_exchange_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (feed_pubkey, _) = get_feed_pda(&program_id, "upd_dup");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "upd_dup".to_string(),
            name: "Upd".to_string(),
            metros: vec![],
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange = Pubkey::new_unique();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateFeed(FeedUpdateArgs {
            name: None,
            metros: Some(vec![
                MetroGroups {
                    exchange,
                    groups: vec![],
                },
                MetroGroups {
                    exchange,
                    groups: vec![],
                },
            ]),
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(65)"),
        "Expected InvalidArgument (Custom(65)), got: {error_string}"
    );
}

#[tokio::test]
async fn test_feed_create_unauthorized_caller_rejected() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let globalstate_pubkey =
        init_globalstate(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let (feed_pubkey, _) = get_feed_pda(&program_id, "unauth");

    // test_payer() is funded but not on the foundation allowlist, so it is not authorized.
    let unauthorized = test_payer();
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
            code: "unauth".to_string(),
            name: "Unauthorized".to_string(),
            metros: vec![],
        }),
        vec![
            AccountMeta::new(feed_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &unauthorized,
    )
    .await;

    // NotAllowed is Custom(8).
    let error_string = format!("{:?}", result.unwrap_err());
    assert!(
        error_string.contains("Custom(8)"),
        "Expected NotAllowed (Custom(8)), got: {error_string}"
    );
}
