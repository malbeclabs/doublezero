//! Issue #2470: each `Close<Kind>AccessPass` instruction must close a pass of its own kind and
//! refuse a pass of any other kind with `AccessPassTypeMismatch`.

mod test_helpers;

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{get_accesspass_pda, get_globalstate_pda, get_program_config_pda},
    processors::accesspass::close::CloseAccessPassArgs,
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::AccountType,
    },
};
use solana_program::rent::Rent;
use solana_program_test::*;
use solana_sdk::{
    account::Account as SolanaAccount, instruction::AccountMeta, pubkey::Pubkey,
    signature::Keypair, signer::Signer,
};
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

/// Starts a fresh `ProgramTest`, runs `InitGlobalState`, and seeds an `AccessPass` account of
/// `pass_type` owned by the payer, with no active connections. The account-building block is
/// lifted from `accesspass_test.rs::test_close_accesspass_rejects_nonzero_connection_count`.
///
/// Uses `test_payer()` rather than the `Keypair` `ProgramTest::start()` generates: that one
/// isn't known until after `start()`, too late to use as the `AccessPass`'s `owner` field,
/// which must be written into the account added before `start()`.
async fn seed_access_pass(
    pass_type: &AccessPassType,
) -> (
    BanksClient,
    Keypair,
    solana_program::hash::Hash,
    Pubkey,
    Pubkey,
    Pubkey,
) {
    let program_id = Pubkey::new_unique();
    let payer = test_payer();

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    let client_ip = Ipv4Addr::new(101, 0, 0, 1);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, bump_seed) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    let seeded_accesspass = AccessPass {
        account_type: AccountType::AccessPass,
        owner: payer.pubkey(),
        bump_seed,
        accesspass_type: pass_type.clone(),
        client_ip,
        user_payer,
        last_access_epoch: 0,
        connection_count: 0,
        status: AccessPassStatus::Requested,
        mgroup_pub_allowlist: vec![],
        mgroup_sub_allowlist: vec![],
        flags: 0,
        tenant_allowlist: vec![],
        unicast_user_count: 0,
        max_unicast_users: 1,
        multicast_user_count: 0,
        max_multicast_users: 1,
    };

    let accesspass_data = borsh::to_vec(&seeded_accesspass).unwrap();
    let rent = Rent::default();
    let lamports = rent.minimum_balance(accesspass_data.len());

    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );
    program_test.add_account(
        accesspass_pubkey,
        SolanaAccount {
            lamports,
            data: accesspass_data,
            owner: program_id,
            executable: false,
            rent_epoch: 0,
        },
    );
    // Fund the payer directly so it can sign InitGlobalState and the close instructions below.
    program_test.add_account(
        payer.pubkey(),
        SolanaAccount {
            lamports: 10_000_000_000,
            data: vec![],
            owner: solana_system_interface::program::ID,
            executable: false,
            rent_epoch: 0,
        },
    );

    let (mut banks_client, _funder, recent_blockhash) = program_test.start().await;

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

    (
        banks_client,
        payer,
        recent_blockhash,
        program_id,
        accesspass_pubkey,
        globalstate_pubkey,
    )
}

#[tokio::test]
async fn close_refuses_a_pass_of_another_kind() {
    for pass_type in [
        AccessPassType::Prepaid,
        AccessPassType::SolanaValidator(Pubkey::new_unique()),
        AccessPassType::SolanaRPC(Pubkey::new_unique()),
        AccessPassType::Others("thing".to_string(), "key".to_string()),
        AccessPassType::EdgeSeat(vec![]),
    ] {
        let (
            mut banks_client,
            payer,
            recent_blockhash,
            program_id,
            accesspass_pubkey,
            globalstate_pubkey,
        ) = seed_access_pass(&pass_type).await;
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
        assert_custom_error(&err, DoubleZeroError::AccessPassTypeMismatch);

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
