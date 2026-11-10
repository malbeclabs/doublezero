use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::{
        accesspass::{
            close::CloseAccessPassArgs, set::SetAccessPassArgs, set_counts::SetAccessPassCountsArgs,
        },
        globalstate::setauthority::SetAuthorityArgs,
    },
    state::accesspass::{AccessPass, AccessPassStatus, AccessPassType},
};
use solana_program_test::*;
use solana_sdk::{
    hash::Hash,
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signature::Keypair,
    signer::Signer,
    transaction::TransactionError,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

// DoubleZeroError codes (see src/error.rs).
const CODE_NOT_ALLOWED: u32 = 8;
const CODE_INVALID_ARGUMENT: u32 = 65;
const CODE_ACCESS_PASS_IN_USE: u32 = 72;
const CODE_ACCESS_PASS_COUNTS_CHANGED: u32 = 134;

/// The counts `SetAccessPassCounts` compares against: connections, unicast users, multicast users.
type Counts = (u16, u16, u16);

fn set_counts_args(expected: Counts, unicast: u16, multicast: u16) -> SetAccessPassCountsArgs {
    SetAccessPassCountsArgs {
        expected_connection_count: expected.0,
        expected_unicast_user_count: expected.1,
        expected_public_multicast_user_count: expected.2,
        unicast_user_count: unicast,
        public_multicast_user_count: multicast,
        private_multicast_user_count: 0,
    }
}

fn assert_custom_error(result: Result<(), BanksClientError>, expected: u32, context: &str) {
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(code),
        ))) if code == expected => {}
        _ => panic!("{context}: expected Custom({expected}), got {result:?}"),
    }
}

struct Env {
    banks_client: BanksClient,
    program_id: Pubkey,
    payer: Keypair,
    recent_blockhash: Hash,
    globalstate_pubkey: Pubkey,
}

async fn setup() -> Env {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;
    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // Makes `payer` the sole foundation_allowlist member, which satisfies ACCESS_PASS_ADMIN.
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

    Env {
        banks_client,
        program_id,
        payer,
        recent_blockhash,
        globalstate_pubkey,
    }
}

impl Env {
    /// Create a prepaid pass at `client_ip` for `user_payer`, signed by `signer`.
    async fn create_pass(
        &mut self,
        client_ip: Ipv4Addr,
        user_payer: Pubkey,
        allow_multiple_ip: bool,
        signer: &Keypair,
    ) -> Pubkey {
        let (pass, _) = get_accesspass_pda(&self.program_id, &client_ip, &user_payer);
        execute_transaction(
            &mut self.banks_client,
            self.recent_blockhash,
            self.program_id,
            DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
                accesspass_type: AccessPassType::Prepaid,
                client_ip,
                last_access_epoch: 9999,
                allow_multiple_ip,
                max_unicast_users: 1,
                max_public_multicast_users: 1,
            }),
            vec![
                AccountMeta::new(pass, false),
                AccountMeta::new(self.globalstate_pubkey, false),
                AccountMeta::new(user_payer, false),
            ],
            signer,
        )
        .await;
        pass
    }

    fn set_counts_accounts(&self, pass: Pubkey) -> Vec<AccountMeta> {
        vec![
            AccountMeta::new(pass, false),
            AccountMeta::new_readonly(self.globalstate_pubkey, false),
        ]
    }

    /// The pass's current counts, which a fresh recount passes as the expected values.
    async fn current_counts(&mut self, pass: Pubkey) -> Counts {
        let ap = self.pass(pass).await.unwrap();
        (
            ap.connection_count,
            ap.unicast_user_count,
            ap.public_multicast_user_count,
        )
    }

    /// Set the counts, expecting the values the pass holds now.
    async fn set_counts(&mut self, pass: Pubkey, unicast: u16, multicast: u16, signer: &Keypair) {
        let expected = self.current_counts(pass).await;
        let accounts = self.set_counts_accounts(pass);
        execute_transaction(
            &mut self.banks_client,
            self.recent_blockhash,
            self.program_id,
            DoubleZeroInstruction::SetAccessPassCounts(set_counts_args(
                expected, unicast, multicast,
            )),
            accounts,
            signer,
        )
        .await;
    }

    /// Like `set_counts`, for a call that must fail for a reason other than stale counts.
    async fn try_set_counts(
        &mut self,
        pass: Pubkey,
        unicast: u16,
        multicast: u16,
        signer: &Keypair,
    ) -> Result<(), BanksClientError> {
        let expected = self.current_counts(pass).await;
        self.try_set_counts_expecting(pass, expected, unicast, multicast, signer)
            .await
    }

    async fn try_set_counts_expecting(
        &mut self,
        pass: Pubkey,
        expected: Counts,
        unicast: u16,
        multicast: u16,
        signer: &Keypair,
    ) -> Result<(), BanksClientError> {
        let accounts = self.set_counts_accounts(pass);
        execute_transaction_expect_failure(
            &mut self.banks_client,
            self.recent_blockhash,
            self.program_id,
            DoubleZeroInstruction::SetAccessPassCounts(set_counts_args(
                expected, unicast, multicast,
            )),
            accounts,
            signer,
        )
        .await
    }

    async fn pass(&mut self, pass: Pubkey) -> Option<AccessPass> {
        get_account_data(&mut self.banks_client, pass)
            .await
            .map(|data| data.get_accesspass().unwrap())
    }

    fn close_accounts(&self, pass: Pubkey) -> Vec<AccountMeta> {
        vec![
            AccountMeta::new(pass, false),
            AccountMeta::new(self.globalstate_pubkey, false),
        ]
    }
}

/// The repair a stale pass needs: counts that keep a pass in use are cleared, and the pass then
/// closes. On the way the connection count follows the sum of the two counts, and the status and
/// client IP follow the rules a user delete applies.
#[tokio::test]
async fn test_set_access_pass_counts_repairs_a_stale_pass() {
    let mut env = setup().await;
    let payer = env.payer.insecure_clone();
    let client_ip = Ipv4Addr::new(100, 0, 0, 1);
    let pass = env
        .create_pass(client_ip, Pubkey::new_unique(), true, &payer)
        .await;

    env.set_counts(pass, 2, 1, &payer).await;
    let ap = env.pass(pass).await.unwrap();
    assert_eq!(ap.connection_count, 3);
    assert_eq!(ap.unicast_user_count, 2);
    assert_eq!(ap.public_multicast_user_count, 1);
    assert_eq!(ap.status, AccessPassStatus::Connected);
    assert_eq!(ap.client_ip, client_ip);

    // A pass that counts users cannot close.
    let accounts = env.close_accounts(pass);
    let result = execute_transaction_expect_failure(
        &mut env.banks_client,
        env.recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::ClosePrepaidAccessPass(CloseAccessPassArgs {}),
        accounts,
        &payer,
    )
    .await;
    assert_custom_error(result, CODE_ACCESS_PASS_IN_USE, "close of a pass in use");

    // Zero counts disconnect the pass, and a multiple-IP pass releases its client IP.
    env.set_counts(pass, 0, 0, &payer).await;
    let ap = env.pass(pass).await.unwrap();
    assert_eq!(ap.connection_count, 0);
    assert_eq!(ap.unicast_user_count, 0);
    assert_eq!(ap.public_multicast_user_count, 0);
    assert_eq!(ap.status, AccessPassStatus::Disconnected);
    assert_eq!(ap.client_ip, Ipv4Addr::UNSPECIFIED);

    let accounts = env.close_accounts(pass);
    execute_transaction(
        &mut env.banks_client,
        env.recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::ClosePrepaidAccessPass(CloseAccessPassArgs {}),
        accounts,
        &payer,
    )
    .await;
    assert!(env.pass(pass).await.is_none(), "the pass must be closed");
}

/// Refusals leave the pass as it was, and zero counts on a pass that never connected keep it
/// `Requested`.
#[tokio::test]
async fn test_set_access_pass_counts_refusals() {
    let mut env = setup().await;
    let payer = env.payer.insecure_clone();
    let pass = env
        .create_pass(
            Ipv4Addr::new(100, 0, 0, 2),
            Pubkey::new_unique(),
            false,
            &payer,
        )
        .await;
    let before = env.pass(pass).await.unwrap();

    // The connection count is the sum of the two counts, so a sum that does not fit is refused.
    let result = env.try_set_counts(pass, u16::MAX, 1, &payer).await;
    assert_custom_error(result, CODE_INVALID_ARGUMENT, "counts that overflow u16");

    // A signer without ACCESS_PASS_ADMIN is refused.
    let outsider = Keypair::new();
    transfer(
        &mut env.banks_client,
        &payer,
        &outsider.pubkey(),
        100_000_000,
    )
    .await;
    let result = env.try_set_counts(pass, 1, 0, &outsider).await;
    assert_custom_error(result, CODE_NOT_ALLOWED, "signer without ACCESS_PASS_ADMIN");

    assert_eq!(env.pass(pass).await.unwrap(), before);

    env.set_counts(pass, 0, 0, &payer).await;
    let ap = env.pass(pass).await.unwrap();
    assert_eq!(ap.status, AccessPassStatus::Requested);
    assert_eq!(ap, before);
}

/// A recount is a snapshot. If a user is created or deleted on the pass before the command lands,
/// the expected counts no longer match, and the program refuses the write instead of losing that
/// user from the counts. Each of the three expected values is checked on its own.
#[tokio::test]
async fn test_set_access_pass_counts_refuses_stale_expected_counts() {
    let mut env = setup().await;
    let payer = env.payer.insecure_clone();
    let pass = env
        .create_pass(
            Ipv4Addr::new(100, 0, 0, 5),
            Pubkey::new_unique(),
            false,
            &payer,
        )
        .await;
    env.set_counts(pass, 2, 1, &payer).await;
    let before = env.pass(pass).await.unwrap();
    assert_eq!(env.current_counts(pass).await, (3, 2, 1));

    for (stale, context) in [
        ((2, 2, 1), "stale connection count"),
        ((3, 1, 1), "stale unicast user count"),
        ((3, 2, 0), "stale multicast user count"),
    ] {
        let result = env
            .try_set_counts_expecting(pass, stale, 0, 0, &payer)
            .await;
        assert_custom_error(result, CODE_ACCESS_PASS_COUNTS_CHANGED, context);
        assert_eq!(env.pass(pass).await.unwrap(), before, "{context}");
    }

    // The same write with the current counts goes through.
    env.set_counts(pass, 0, 0, &payer).await;
    assert_eq!(env.current_counts(pass).await, (0, 0, 0));
}

/// The feed authority holds ACCESS_PASS_ADMIN, so only the ownership guard stops it on a pass it
/// did not create. The positive control on its own pass shows the refusal comes from that guard.
#[tokio::test]
async fn test_set_access_pass_counts_feed_authority_limited_to_own_passes() {
    let mut env = setup().await;
    let payer = env.payer.insecure_clone();

    let feed = Keypair::new();
    transfer(
        &mut env.banks_client,
        &payer,
        &feed.pubkey(),
        10_000_000_000,
    )
    .await;
    execute_transaction(
        &mut env.banks_client,
        env.recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
            feed_authority_pk: Some(feed.pubkey()),
            ..Default::default()
        }),
        vec![AccountMeta::new(env.globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let foundation_pass = env
        .create_pass(Ipv4Addr::new(100, 0, 0, 3), payer.pubkey(), false, &payer)
        .await;
    let result = env.try_set_counts(foundation_pass, 1, 0, &feed).await;
    assert_custom_error(
        result,
        CODE_NOT_ALLOWED,
        "feed authority on a pass it does not own",
    );
    assert_eq!(env.pass(foundation_pass).await.unwrap().connection_count, 0);

    let feed_pass = env
        .create_pass(
            Ipv4Addr::new(100, 0, 0, 4),
            Pubkey::new_unique(),
            false,
            &feed,
        )
        .await;
    env.set_counts(feed_pass, 1, 0, &feed).await;
    let ap = env.pass(feed_pass).await.unwrap();
    assert_eq!(ap.owner, feed.pubkey());
    assert_eq!(ap.connection_count, 1);
}

/// The test sends 1 public multicast user and 1 private multicast user. The program sets the
/// connection count to 2 and the public multicast count to 1. The pass stores no private count.
#[tokio::test]
async fn test_set_access_pass_counts_adds_private_multicast_users_to_connections() {
    let mut env = setup().await;
    let payer = env.payer.insecure_clone();
    let pass = env
        .create_pass(
            Ipv4Addr::new(100, 0, 0, 3),
            Pubkey::new_unique(),
            false,
            &payer,
        )
        .await;

    let expected = env.current_counts(pass).await;
    let accounts = env.set_counts_accounts(pass);
    execute_transaction(
        &mut env.banks_client,
        env.recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::SetAccessPassCounts(SetAccessPassCountsArgs {
            private_multicast_user_count: 1,
            ..set_counts_args(expected, 0, 1)
        }),
        accounts,
        &payer,
    )
    .await;

    let ap = env.pass(pass).await.unwrap();
    assert_eq!(ap.connection_count, 2);
    assert_eq!(ap.unicast_user_count, 0);
    assert_eq!(ap.public_multicast_user_count, 1);
}
