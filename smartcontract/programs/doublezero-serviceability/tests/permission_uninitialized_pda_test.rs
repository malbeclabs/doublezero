//! A payer's Permission PDA that has never been created (or was closed) is treated by
//! `authorize()` exactly like no Permission account at all, so clients can append it
//! unconditionally and offline. These tests drive the real program through an
//! `authorize()`-gated instruction with that uninitialized PDA as the trailing account.

use borsh::to_vec;
use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::*,
    pda::{get_link_pda, get_permission_pda},
    processors::{
        allowlist::qa::add::AddQaAllowlistArgs,
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        permission::create::PermissionCreateArgs,
        topology::{clear::TopologyClearArgs, create::TopologyCreateArgs},
    },
    state::{
        feature_flags::FeatureFlag,
        link::{Link, LinkStatus},
        permission::permission_flags,
        topology::{TopologyConstraint, TopologyInfo},
    },
};
use doublezero_serviceability_instruction as dzi;
use solana_program_test::*;
use solana_sdk::{
    account::Account,
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    rent::Rent,
    signature::{Keypair, Signer},
    transaction::{Transaction, TransactionError},
};

mod test_helpers;
use test_helpers::*;

/// `AddQaAllowlist` (GLOBALSTATE_ADMIN, legacy: foundation allowlist) with `permission`
/// as the trailing account after `[payer, system]` — the layout the SDK emits.
async fn add_qa_allowlist(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &Keypair,
    permission: Pubkey,
) -> Result<(), BanksClientError> {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction_with_extra_accounts(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddQaAllowlist(AddQaAllowlistArgs {
            pubkey: Pubkey::new_unique(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
        &[AccountMeta::new_readonly(permission, false)],
    )
    .await
}

async fn enable_strict_mode(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &Keypair,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::RequirePermissionAccounts.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
    )
    .await;
}

#[tokio::test]
async fn test_uninitialized_permission_pda_strict_off_falls_back_to_legacy() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());
    assert!(account_is_closed(&mut banks_client, permission_pda).await);

    // Never-created PDA: the foundation payer is authorized via the legacy allowlist.
    add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap();

    // Pre-funded by a third party (still system-owned, no data): same outcome, so a
    // lamport transfer cannot be used to grief the payer into a hard failure.
    transfer(&mut banks_client, &payer, &permission_pda, 1_000_000).await;
    let account = banks_client
        .get_account(permission_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, solana_system_interface::program::ID);
    assert!(account.data.is_empty());
    add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap();

    // A key with no legacy authority is still denied.
    let outsider = Keypair::new();
    transfer(&mut banks_client, &payer, &outsider.pubkey(), 1_000_000_000).await;
    let (outsider_pda, _) = get_permission_pda(&program_id, &outsider.pubkey());
    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &outsider,
        outsider_pda,
    )
    .await
    .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);
}

#[tokio::test]
async fn test_uninitialized_permission_pda_wrong_pda_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    // Someone else's (uninitialized) Permission PDA is not the payer's.
    let (other_pda, _) = get_permission_pda(&program_id, &Pubkey::new_unique());

    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        other_pda,
    )
    .await
    .unwrap_err();
    assert!(
        matches!(
            err,
            BanksClientError::TransactionError(TransactionError::InstructionError(
                0,
                InstructionError::InvalidArgument
            ))
        ),
        "expected InvalidArgument, got {err:?}"
    );
}

#[tokio::test]
async fn test_uninitialized_permission_pda_strict_on() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());

    enable_strict_mode(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // Strict mode: the legacy allowlist no longer authorizes, with or without lamports.
    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    transfer(&mut banks_client, &payer, &permission_pda, 1_000_000).await;
    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    // ...except the foundation PERMISSION_ADMIN recovery: a foundation member can still
    // create Permission accounts — here its own, whose PDA is both the account being
    // created and the (still uninitialized, pre-funded) trailing Permission account.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: payer.pubkey(),
            permissions: permission_flags::GLOBALSTATE_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
        &[AccountMeta::new_readonly(permission_pda, false)],
    )
    .await
    .unwrap();

    // With the Permission account now initialized, the same instruction succeeds.
    add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap();
}

// ── Variable-length leading account list ─────────────────────────────────────
//
// `ClearTopology` takes `[topology, globalstate, link..]` ahead of `[payer, system]`, and
// `split_trailing_permission` peels the trailing Permission PDA off by address alone. These
// tests send that layout as the instruction builder emits it — links, then payer, system and
// the payer's uninitialized PDA — and check every link is read as a link.

const TOPOLOGY: &str = "topo-a";

async fn submit(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    ix: Instruction,
) -> Result<(), BanksClientError> {
    let mut all = dzi::compute_budget_prelude().to_vec();
    all.push(ix);
    let mut tx = Transaction::new_with_payer(&all, Some(&payer.pubkey()));
    let blockhash = wait_for_new_blockhash(banks_client).await;
    tx.sign(&[payer], blockhash);
    banks_client.process_transaction(tx).await
}

/// Seeds `count` links tagged into `topology` and sets the topology's `reference_count` to
/// match. Seeded rather than created so the test needs no devices or contributors: the
/// processor only reads each link and edits its `link_topologies`.
async fn seed_tagged_links(
    context: &mut ProgramTestContext,
    program_id: Pubkey,
    topology: Pubkey,
    count: u128,
) -> Vec<Pubkey> {
    let mut links = Vec::new();
    for index in 1000..1000 + count {
        let (pubkey, bump_seed) = get_link_pda(&program_id, index);
        let data = to_vec(&Link {
            index,
            bump_seed,
            code: format!("link{index}"),
            status: LinkStatus::Activated,
            // Values `Link::validate` accepts, since the processor rewrites each link.
            tunnel_net: "172.16.0.0/31".parse().unwrap(),
            bandwidth: 10_000_000_000,
            delay_ns: 1_000_000,
            jitter_ns: 100_000,
            link_topologies: vec![topology],
            ..Link::default()
        })
        .unwrap();
        context.set_account(
            &pubkey,
            &Account {
                lamports: Rent::default().minimum_balance(data.len()),
                data,
                owner: program_id,
                executable: false,
                rent_epoch: 0,
            }
            .into(),
        );
        links.push(pubkey);
    }

    let mut account = context
        .banks_client
        .get_account(topology)
        .await
        .unwrap()
        .expect("topology");
    let mut info = TopologyInfo::try_from(&account.data[..]).unwrap();
    info.reference_count = count as u32;
    account.data = to_vec(&info).unwrap();
    context.set_account(&topology, &account.into());
    links
}

async fn link_topologies(banks_client: &mut BanksClient, link: Pubkey) -> Vec<Pubkey> {
    get_account_data(banks_client, link)
        .await
        .expect("link")
        .get_tunnel()
        .unwrap()
        .link_topologies
}

async fn topology_reference_count(banks_client: &mut BanksClient, topology: Pubkey) -> u32 {
    get_account_data(banks_client, topology)
        .await
        .expect("topology")
        .get_topology()
        .unwrap()
        .reference_count
}

/// Creates `TOPOLOGY` and returns (context, payer, program_id, globalstate, topology).
async fn setup_topology() -> (ProgramTestContext, Keypair, Pubkey, Pubkey, Pubkey) {
    let (mut context, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig_context().await;
    let payer = context.payer.insecure_clone();
    let ix = dzi::topology::create_topology(
        &program_id,
        &payer.pubkey(),
        TopologyCreateArgs {
            name: TOPOLOGY.to_string(),
            constraint: TopologyConstraint::IncludeAny,
        },
    );
    let topology = ix.accounts[0].pubkey;
    submit(&mut context.banks_client, &payer, ix).await.unwrap();
    (context, payer, program_id, globalstate_pubkey, topology)
}

fn clear_ix(program_id: &Pubkey, payer: &Pubkey, links: &[Pubkey]) -> Instruction {
    let ix = dzi::topology::clear_topology(
        program_id,
        payer,
        links,
        TopologyClearArgs {
            name: TOPOLOGY.to_string(),
        },
    );
    // [topology, globalstate, link.., payer, system, permission]
    assert_eq!(ix.accounts.len(), links.len() + 5);
    assert_eq!(
        ix.accounts.last().unwrap().pubkey,
        get_permission_pda(program_id, payer).0
    );
    ix
}

#[tokio::test]
async fn test_uninitialized_permission_pda_behind_link_list_strict_off() {
    let (mut context, payer, program_id, _, topology) = setup_topology().await;
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());

    // Never-created PDA, then pre-funded by a third party (still system-owned, no data):
    // either way the foundation payer is authorized via the legacy allowlist and every
    // link in the list is cleared.
    for prefund in [false, true] {
        if prefund {
            transfer(
                &mut context.banks_client,
                &payer,
                &permission_pda,
                1_000_000,
            )
            .await;
            let account = context
                .banks_client
                .get_account(permission_pda)
                .await
                .unwrap()
                .unwrap();
            assert_eq!(account.owner, solana_system_interface::program::ID);
            assert!(account.data.is_empty());
        } else {
            assert!(account_is_closed(&mut context.banks_client, permission_pda).await);
        }

        let links = seed_tagged_links(&mut context, program_id, topology, 3).await;
        assert_eq!(
            topology_reference_count(&mut context.banks_client, topology).await,
            3
        );

        let ix = clear_ix(&program_id, &payer.pubkey(), &links);
        submit(&mut context.banks_client, &payer, ix).await.unwrap();

        for link in links {
            assert!(link_topologies(&mut context.banks_client, link)
                .await
                .is_empty());
        }
        assert_eq!(
            topology_reference_count(&mut context.banks_client, topology).await,
            0
        );
    }
}

#[tokio::test]
async fn test_uninitialized_permission_pda_behind_link_list_strict_on() {
    let (mut context, payer, program_id, globalstate_pubkey, topology) = setup_topology().await;
    let links = seed_tagged_links(&mut context, program_id, topology, 3).await;

    enable_strict_mode(
        &mut context.banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
    )
    .await;

    // A non-foundation signer with an uninitialized PDA, unfunded and then pre-funded.
    let outsider = Keypair::new();
    transfer(
        &mut context.banks_client,
        &payer,
        &outsider.pubkey(),
        1_000_000_000,
    )
    .await;
    let (outsider_pda, _) = get_permission_pda(&program_id, &outsider.pubkey());
    for prefund in [false, true] {
        if prefund {
            transfer(&mut context.banks_client, &payer, &outsider_pda, 1_000_000).await;
        }
        let ix = clear_ix(&program_id, &outsider.pubkey(), &links);
        let err = submit(&mut context.banks_client, &outsider, ix)
            .await
            .unwrap_err();
        assert_custom_error(&err, DoubleZeroError::NotAllowed);
    }

    // Strict mode also withdraws the foundation payer's legacy authority for this flag.
    let ix = clear_ix(&program_id, &payer.pubkey(), &links);
    let err = submit(&mut context.banks_client, &payer, ix)
        .await
        .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    for link in links {
        assert_eq!(
            link_topologies(&mut context.banks_client, link).await,
            vec![topology]
        );
    }
    assert_eq!(
        topology_reference_count(&mut context.banks_client, topology).await,
        3
    );
}
