//! Issue #2470: each `Close<Kind>AccessPass` instruction must close a pass of its own kind and
//! refuse a pass of any other kind with `InvalidAccessPassType`.

mod test_helpers;

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{get_accesspass_pda, get_globalstate_pda, get_program_config_pda},
    processors::accesspass::{close::CloseAccessPassArgs, set::SetAccessPassArgs},
    state::accesspass::AccessPassType,
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Keypair};
use std::net::Ipv4Addr;
use test_helpers::*;

/// The close instruction that matches each pass type, and one that does not.
fn close_instructions(
    pass_type: &AccessPassType,
) -> (DoubleZeroInstruction, DoubleZeroInstruction) {
    let args = CloseAccessPassArgs {};
    match pass_type {
        AccessPassType::Prepaid => (
            DoubleZeroInstruction::ClosePrepaidAccessPass(args.clone()),
            DoubleZeroInstruction::CloseEdgeSeatAccessPass(args),
        ),
        AccessPassType::SolanaValidator(_) => (
            DoubleZeroInstruction::CloseSolanaValidatorAccessPass(args.clone()),
            DoubleZeroInstruction::ClosePrepaidAccessPass(args),
        ),
        AccessPassType::SolanaRPC(_) => (
            DoubleZeroInstruction::CloseSolanaRPCAccessPass(args.clone()),
            DoubleZeroInstruction::ClosePrepaidAccessPass(args),
        ),
        AccessPassType::Others(_, _) => (
            DoubleZeroInstruction::CloseOthersAccessPass(args.clone()),
            DoubleZeroInstruction::ClosePrepaidAccessPass(args),
        ),
        AccessPassType::EdgeSeat(_) => (
            DoubleZeroInstruction::CloseEdgeSeatAccessPass(args.clone()),
            DoubleZeroInstruction::ClosePrepaidAccessPass(args),
        ),
    }
}

/// Create an access pass of `pass_type` at `client_ip` via `SetAccessPass`, the real
/// instruction a caller uses, rather than hand-writing an `AccessPass` and inserting it into
/// the test bank. `SetAccessPass` always creates a fresh pass with `connection_count: 0`,
/// which is what the close path requires. Returns its pubkey.
async fn create_access_pass(
    banks_client: &mut BanksClient,
    recent_blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &Keypair,
    client_ip: Ipv4Addr,
    pass_type: AccessPassType,
) -> Pubkey {
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: pass_type,
            client_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        payer,
    )
    .await;

    accesspass_pubkey
}

#[tokio::test]
async fn close_refuses_a_pass_of_another_kind() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    // Makes `payer` the sole entry in the foundation allowlist, so it holds ACCESS_PASS_ADMIN.
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

    for (i, pass_type) in [
        AccessPassType::Prepaid,
        AccessPassType::SolanaValidator(Pubkey::new_unique()),
        AccessPassType::SolanaRPC(Pubkey::new_unique()),
        AccessPassType::Others("thing".to_string(), "key".to_string()),
        AccessPassType::EdgeSeat(vec![]),
    ]
    .into_iter()
    .enumerate()
    {
        let client_ip: Ipv4Addr = [101, 0, 0, 1 + i as u8].into();
        let accesspass_pubkey = create_access_pass(
            &mut banks_client,
            recent_blockhash,
            program_id,
            globalstate_pubkey,
            &payer,
            client_ip,
            pass_type.clone(),
        )
        .await;

        let accounts = vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];
        let (matching, other) = close_instructions(&pass_type);

        let err = try_execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            other,
            accounts.clone(),
            &payer,
        )
        .await
        .expect_err("a close for another kind must fail");
        assert_custom_error(&err, DoubleZeroError::InvalidAccessPassType);

        assert!(
            get_account_data(&mut banks_client, accesspass_pubkey)
                .await
                .is_some(),
            "the pass must survive a refused close: {pass_type}"
        );

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            matching,
            accounts,
            &payer,
        )
        .await;

        assert!(
            get_account_data(&mut banks_client, accesspass_pubkey)
                .await
                .is_none(),
            "the matching close must remove the pass: {pass_type}"
        );
    }
}
