//! The feed authority's access-pass rules after it moves to a Permission account
//! (malbeclabs/doublezero#4092).
//!
//! The feed authority may only change or close passes it owns, and it may add or remove a group on
//! a pass whose `user_payer` differs from the one in the instruction. These tests set up the
//! signer as it is after the migration: it holds `ACCESS_PASS_ADMIN | FEED_AUTHORITY` through its
//! Permission account, `feed_authority_pk` does not name it, and strict mode is on. Keyed on
//! `feed_authority_pk` alone, every ownership check below would pass.

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::*,
    pda::*,
    processors::{
        accesspass::{
            close::CloseAccessPassArgs, set::SetAccessPassArgs, set_flags::SetAccessPassFlagsArgs,
        },
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        multicastgroup::{
            allowlist::subscriber::{
                add::AddMulticastGroupSubAllowlistArgs,
                remove::RemoveMulticastGroupSubAllowlistArgs,
            },
            create::MulticastGroupCreateArgs,
        },
        permission::create::PermissionCreateArgs,
    },
    resource::ResourceType,
    state::{accesspass::AccessPassType, feature_flags::FeatureFlag, permission::permission_flags},
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Keypair, signer::Signer};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

struct Setup {
    banks_client: BanksClient,
    program_id: Pubkey,
    globalstate: Pubkey,
    mgroup: Pubkey,
    oracle: Keypair,
    oracle_permission: Pubkey,
    /// A pass the foundation payer created and owns.
    foreign_pass: Pubkey,
    foreign_ip: Ipv4Addr,
    foreign_user_payer: Pubkey,
    /// A pass the oracle created and owns.
    own_pass: Pubkey,
    own_ip: Ipv4Addr,
    own_user_payer: Pubkey,
}

async fn setup() -> Setup {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let (globalstate, _) = get_globalstate_pda(&program_id);
    init_globalstate_and_config(&mut banks_client, program_id, &payer, recent_blockhash).await;

    let gs = get_account_data(&mut banks_client, globalstate)
        .await
        .expect("globalstate")
        .get_global_state()
        .unwrap();
    assert_eq!(gs.feed_authority_pk, Pubkey::default());

    let (mgroup, _) = get_multicastgroup_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "feedperm".to_string(),
            max_bandwidth: 1_000_000_000,
            owner: payer.pubkey(),
            use_onchain_allocation: true,
            bond_ref: Pubkey::default(),
        }),
        vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    // The oracle as it is after the migration: no legacy key, only a Permission account.
    let oracle = Keypair::new();
    transfer(&mut banks_client, &payer, &oracle.pubkey(), 10_000_000_000).await;
    let (oracle_permission, _) = get_permission_pda(&program_id, &oracle.pubkey());
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: oracle.pubkey(),
            permissions: permission_flags::ACCESS_PASS_ADMIN | permission_flags::FEED_AUTHORITY,
        }),
        vec![
            AccountMeta::new(oracle_permission, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        &payer,
    )
    .await;

    let foreign_ip = Ipv4Addr::new(100, 0, 0, 30);
    let foreign_user_payer = payer.pubkey();
    let (foreign_pass, _) = get_accesspass_pda(&program_id, &foreign_ip, &foreign_user_payer);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        set_access_pass(foreign_ip),
        vec![
            AccountMeta::new(foreign_pass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(foreign_user_payer, false),
        ],
        &payer,
    )
    .await;

    let own_ip = Ipv4Addr::new(100, 0, 0, 31);
    let own_user_payer = Pubkey::new_unique();
    let (own_pass, _) = get_accesspass_pda(&program_id, &own_ip, &own_user_payer);
    try_execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        set_access_pass(own_ip),
        vec![
            AccountMeta::new(own_pass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(own_user_payer, false),
        ],
        &oracle,
        &[AccountMeta::new_readonly(oracle_permission, false)],
    )
    .await
    .expect("the oracle creates its own pass");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::RequirePermissionAccounts.to_mask(),
        }),
        vec![AccountMeta::new(globalstate, false)],
        &payer,
    )
    .await;

    Setup {
        banks_client,
        program_id,
        globalstate,
        mgroup,
        oracle,
        oracle_permission,
        foreign_pass,
        foreign_ip,
        foreign_user_payer,
        own_pass,
        own_ip,
        own_user_payer,
    }
}

fn set_access_pass(client_ip: Ipv4Addr) -> DoubleZeroInstruction {
    DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
        accesspass_type: AccessPassType::Prepaid,
        client_ip,
        last_access_epoch: 9999,
        allow_multiple_ip: false,
        max_unicast_users: 1,
        max_multicast_users: 1,
    })
}

fn lock_flags() -> DoubleZeroInstruction {
    DoubleZeroInstruction::SetAccessPassFlags(SetAccessPassFlagsArgs {
        allow_multiple_ip: None,
        dzf_locked: Some(true),
    })
}

impl Setup {
    /// Runs `instruction` signed by the oracle, with its Permission account appended.
    async fn as_oracle(
        &mut self,
        instruction: DoubleZeroInstruction,
        accounts: Vec<AccountMeta>,
    ) -> Result<(), BanksClientError> {
        let blockhash = self.banks_client.get_latest_blockhash().await.unwrap();
        try_execute_transaction_with_extra_accounts(
            &mut self.banks_client,
            blockhash,
            self.program_id,
            instruction,
            accounts,
            &self.oracle,
            &[AccountMeta::new_readonly(self.oracle_permission, false)],
        )
        .await
    }

    async fn sub_allowlist(&mut self, pass: Pubkey) -> Vec<Pubkey> {
        get_account_data(&mut self.banks_client, pass)
            .await
            .expect("access pass")
            .get_accesspass()
            .unwrap()
            .mgroup_sub_allowlist
    }
}

#[tokio::test]
async fn test_feed_authority_permission_cannot_change_a_pass_it_does_not_own() {
    let mut s = setup().await;
    let (pass, gs, ip, user_payer) = (
        s.foreign_pass,
        s.globalstate,
        s.foreign_ip,
        s.foreign_user_payer,
    );

    let err = s
        .as_oracle(
            lock_flags(),
            vec![
                AccountMeta::new(pass, false),
                AccountMeta::new_readonly(gs, false),
            ],
        )
        .await
        .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    let err = s
        .as_oracle(
            set_access_pass(ip),
            vec![
                AccountMeta::new(pass, false),
                AccountMeta::new(gs, false),
                AccountMeta::new(user_payer, false),
            ],
        )
        .await
        .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    let mgroup = s.mgroup;
    let err = s
        .as_oracle(
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
                    client_ip: ip,
                    user_payer,
                },
            ),
            vec![
                AccountMeta::new(mgroup, false),
                AccountMeta::new(pass, false),
                AccountMeta::new(gs, false),
            ],
        )
        .await
        .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    let err = s
        .as_oracle(
            DoubleZeroInstruction::ClosePrepaidAccessPass(CloseAccessPassArgs {}),
            vec![AccountMeta::new(pass, false), AccountMeta::new(gs, false)],
        )
        .await
        .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    let ap = get_account_data(&mut s.banks_client, pass)
        .await
        .expect("the pass is not closed")
        .get_accesspass()
        .unwrap();
    assert!(!ap.dzf_locked());
    assert!(ap.mgroup_sub_allowlist.is_empty());
}

/// Positive control for the test above: the same signer, on a pass it owns, gets through every
/// instruction, so the rejections come from the ownership check and not from `authorize()`. It also
/// keeps the feed authority's leave to name a different `user_payer` on the subscriber allowlist.
#[tokio::test]
async fn test_feed_authority_permission_keeps_its_rights_on_its_own_pass() {
    let mut s = setup().await;
    let (pass, gs, ip, mgroup) = (s.own_pass, s.globalstate, s.own_ip, s.mgroup);
    let other_user_payer = Pubkey::new_unique();
    assert_ne!(other_user_payer, s.own_user_payer);

    s.as_oracle(
        lock_flags(),
        vec![
            AccountMeta::new(pass, false),
            AccountMeta::new_readonly(gs, false),
        ],
    )
    .await
    .expect("the oracle locks its own pass");

    s.as_oracle(
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(AddMulticastGroupSubAllowlistArgs {
            client_ip: ip,
            user_payer: other_user_payer,
        }),
        vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(pass, false),
            AccountMeta::new(gs, false),
        ],
    )
    .await
    .expect("the feed authority may name another user_payer");
    assert_eq!(s.sub_allowlist(pass).await, vec![mgroup]);

    s.as_oracle(
        DoubleZeroInstruction::RemoveMulticastGroupSubAllowlist(
            RemoveMulticastGroupSubAllowlistArgs {
                client_ip: ip,
                user_payer: other_user_payer,
            },
        ),
        vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(pass, false),
            AccountMeta::new(gs, false),
        ],
    )
    .await
    .expect("the feed authority may name another user_payer");
    assert!(s.sub_allowlist(pass).await.is_empty());

    s.as_oracle(
        DoubleZeroInstruction::ClosePrepaidAccessPass(CloseAccessPassArgs {}),
        vec![AccountMeta::new(pass, false), AccountMeta::new(gs, false)],
    )
    .await
    .expect("the oracle closes its own pass");
    assert!(account_is_closed(&mut s.banks_client, pass).await);
}
