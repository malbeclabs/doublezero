//! The instruction builders append the payer's Permission PDA to every `authorize()`-gated
//! instruction, whether or not the account exists. These tests drive the processors whose
//! optional accounts are detected by account count or by position — the ones a trailing account
//! could confuse — through the builders, for both an uninitialized and an initialized
//! Permission account.
//!
//! Each case uses a non-foundation signer whose only possible grant is its Permission account, so
//! the outcome proves where the processor put the trailing account: denied while the PDA is
//! uninitialized (it reached `authorize()` as "no permission", not a tenant or user-payer slot),
//! and accepted once the Permission account exists (it reached `authorize()` as the permission).
//! The foundation payer, authorized by the legacy allowlist, shows the optional slots themselves
//! still parse with the uninitialized PDA behind them.

use doublezero_serviceability::{
    error::DoubleZeroError,
    pda::{get_accesspass_pda, get_permission_pda, get_tenant_pda},
    processors::{
        accesspass::set::SetAccessPassArgs,
        multicastgroup::{
            allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs,
            create::MulticastGroupCreateArgs,
        },
        permission::create::PermissionCreateArgs,
        tenant::create::TenantCreateArgs,
    },
    state::{accesspass::AccessPassType, permission::permission_flags},
};
use doublezero_serviceability_instruction as dzi;
use solana_program_test::*;
use solana_sdk::{
    instruction::Instruction,
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::Transaction,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

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

/// A funded signer with no legacy authority.
async fn new_admin(banks_client: &mut BanksClient, funder: &Keypair) -> Keypair {
    let admin = Keypair::new();
    transfer(banks_client, funder, &admin.pubkey(), 10_000_000_000).await;
    admin
}

async fn grant(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    foundation: &Keypair,
    user_payer: Pubkey,
    permissions: u128,
) {
    let ix = dzi::permission::create_permission(
        &program_id,
        &foundation.pubkey(),
        PermissionCreateArgs {
            user_payer,
            permissions,
        },
    );
    submit(banks_client, foundation, ix).await.unwrap();
}

async fn assert_uninitialized(banks_client: &mut BanksClient, program_id: Pubkey, key: &Pubkey) {
    let (pda, _) = get_permission_pda(&program_id, key);
    assert!(account_is_closed(banks_client, pda).await);
}

/// `SetAccessPass` detects the optional `[tenant_remove, tenant_add]` pair with `accounts.len() >= 7`.
/// With the trailing PDA a tenant change carries 8 accounts and a plain set 6.
#[tokio::test]
async fn test_set_access_pass_tenant_pair_with_trailing_permission_pda() {
    let (mut banks_client, payer, program_id, _, _) = setup_program_with_globalconfig().await;

    let tenant_ix = dzi::tenant::create_tenant(
        &program_id,
        &payer.pubkey(),
        TenantCreateArgs {
            code: "acme".to_string(),
            administrator: payer.pubkey(),
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        },
    );
    submit(&mut banks_client, &payer, tenant_ix).await.unwrap();
    let (tenant, _) = get_tenant_pda(&program_id, "acme");

    let args = |client_ip| SetAccessPassArgs {
        accesspass_type: AccessPassType::Prepaid,
        client_ip,
        last_access_epoch: 10,
        allow_multiple_ip: false,
        max_unicast_users: 1,
        max_multicast_users: 1,
    };
    let tenant_of = |banks_client: &mut BanksClient, accesspass: Pubkey| {
        let mut banks_client = banks_client.clone();
        async move {
            get_account_data(&mut banks_client, accesspass)
                .await
                .expect("access pass")
                .get_accesspass()
                .unwrap()
                .tenant_allowlist
        }
    };

    // Foundation payer, uninitialized PDA, tenant pair: 8 accounts, the tenant is applied.
    assert_uninitialized(&mut banks_client, program_id, &payer.pubkey()).await;
    let user_payer = Pubkey::new_unique();
    let ip = Ipv4Addr::new(100, 0, 0, 1);
    let ix = dzi::accesspass::set_access_pass(
        &program_id,
        &payer.pubkey(),
        &user_payer,
        &Pubkey::default(),
        &tenant,
        args(ip),
    );
    assert_eq!(ix.accounts.len(), 8);
    submit(&mut banks_client, &payer, ix).await.unwrap();
    let (accesspass, _) = get_accesspass_pda(&program_id, &ip, &user_payer);
    assert_eq!(tenant_of(&mut banks_client, accesspass).await, vec![tenant]);

    // A non-foundation signer with an uninitialized PDA is denied, with the pair (removing the
    // tenant from that pass) and without it.
    let admin = new_admin(&mut banks_client, &payer).await;
    assert_uninitialized(&mut banks_client, program_id, &admin.pubkey()).await;
    let ip2 = Ipv4Addr::new(100, 0, 0, 2);
    for (current, new, client_ip) in [
        (tenant, Pubkey::default(), ip),
        (Pubkey::default(), Pubkey::default(), ip2),
    ] {
        let ix = dzi::accesspass::set_access_pass(
            &program_id,
            &admin.pubkey(),
            &user_payer,
            &current,
            &new,
            args(client_ip),
        );
        let err = submit(&mut banks_client, &admin, ix).await.unwrap_err();
        assert_custom_error(&err, DoubleZeroError::NotAllowed);
    }

    // Once its ACCESS_PASS_ADMIN Permission exists, the 8-account tenant removal succeeds and
    // the pair is still read as the tenant pair: the tenant is gone from the pass.
    grant(
        &mut banks_client,
        program_id,
        &payer,
        admin.pubkey(),
        permission_flags::ACCESS_PASS_ADMIN,
    )
    .await;
    let ix = dzi::accesspass::set_access_pass(
        &program_id,
        &admin.pubkey(),
        &user_payer,
        &tenant,
        &Pubkey::default(),
        args(ip),
    );
    assert_eq!(ix.accounts.len(), 8);
    submit(&mut banks_client, &admin, ix).await.unwrap();
    assert!(tenant_of(&mut banks_client, accesspass).await.is_empty());

    // Adding a tenant also needs the signer to administer it; with the Permission account the
    // instruction now gets past `authorize()` to that check, which only a correctly parsed
    // tenant pair can reach.
    let ix = dzi::accesspass::set_access_pass(
        &program_id,
        &admin.pubkey(),
        &user_payer,
        &Pubkey::default(),
        &tenant,
        args(ip2),
    );
    let err = submit(&mut banks_client, &admin, ix).await.unwrap_err();
    assert_custom_error(&err, DoubleZeroError::Unauthorized);

    // And the 6-account shape (no tenant change) with the initialized PDA.
    let ip3 = Ipv4Addr::new(100, 0, 0, 3);
    let ix = dzi::accesspass::set_access_pass(
        &program_id,
        &admin.pubkey(),
        &user_payer,
        &Pubkey::default(),
        &Pubkey::default(),
        args(ip3),
    );
    assert_eq!(ix.accounts.len(), 6);
    submit(&mut banks_client, &admin, ix).await.unwrap();
    let (accesspass3, _) = get_accesspass_pda(&program_id, &ip3, &user_payer);
    assert!(tenant_of(&mut banks_client, accesspass3).await.is_empty());
}

/// `AddMulticastGroupPubAllowlist` carries an optional `user_payer` ahead of `[payer, system]`
/// and tells it from the Permission account by PDA match.
#[tokio::test]
async fn test_pub_allowlist_user_payer_with_trailing_permission_pda() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;

    let account_index = get_globalstate(&mut banks_client, globalstate_pubkey)
        .await
        .account_index
        + 1;
    let create = dzi::multicastgroup::create_multicast_group(
        &program_id,
        &payer.pubkey(),
        account_index,
        MulticastGroupCreateArgs {
            code: "mg01".to_string(),
            max_bandwidth: 1_000_000_000,
            owner: payer.pubkey(),
            use_onchain_allocation: true,
            stake_ref: Pubkey::default(),
        },
    );
    let mgroup = create.accounts[0].pubkey;
    submit(&mut banks_client, &payer, create).await.unwrap();

    let add = |signer: &Pubkey, user_payer: Pubkey, client_ip: Ipv4Addr| {
        let (accesspass, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);
        let ix = dzi::multicastgroup::add_multicast_group_pub_allowlist(
            &program_id,
            signer,
            &mgroup,
            &accesspass,
            AddMulticastGroupPubAllowlistArgs {
                client_ip,
                user_payer,
            },
        );
        (ix, accesspass)
    };
    let pub_allowlist = |banks_client: &mut BanksClient, accesspass: Pubkey| {
        let mut banks_client = banks_client.clone();
        async move {
            get_account_data(&mut banks_client, accesspass)
                .await
                .expect("access pass")
                .get_accesspass()
                .unwrap()
                .mgroup_pub_allowlist
        }
    };

    // The foundation payer (the group owner) with an uninitialized PDA.
    assert_uninitialized(&mut banks_client, program_id, &payer.pubkey()).await;
    let (ix, accesspass) = add(
        &payer.pubkey(),
        Pubkey::new_unique(),
        Ipv4Addr::new(100, 0, 1, 1),
    );
    submit(&mut banks_client, &payer, ix).await.unwrap();
    assert_eq!(
        pub_allowlist(&mut banks_client, accesspass).await,
        vec![mgroup]
    );

    // A non-owner, non-foundation signer: denied with the uninitialized PDA...
    let admin = new_admin(&mut banks_client, &payer).await;
    assert_uninitialized(&mut banks_client, program_id, &admin.pubkey()).await;
    let user_payer = Pubkey::new_unique();
    let ip = Ipv4Addr::new(100, 0, 1, 2);
    let (ix, _) = add(&admin.pubkey(), user_payer, ip);
    let err = submit(&mut banks_client, &admin, ix).await.unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    // ...and accepted once its MULTICAST_ADMIN Permission exists.
    grant(
        &mut banks_client,
        program_id,
        &payer,
        admin.pubkey(),
        permission_flags::MULTICAST_ADMIN,
    )
    .await;
    let (ix, accesspass) = add(&admin.pubkey(), user_payer, ip);
    submit(&mut banks_client, &admin, ix).await.unwrap();
    assert_eq!(
        pub_allowlist(&mut banks_client, accesspass).await,
        vec![mgroup]
    );
}
