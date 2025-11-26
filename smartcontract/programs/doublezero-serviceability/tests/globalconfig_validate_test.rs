use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::*, pda::*, processors::globalconfig::set::SetGlobalConfigArgs,
};
use solana_program_test::*;
use solana_sdk::instruction::AccountMeta;

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn set_globalconfig_invalid_asn_should_fail() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    // 1. Initialize global state so that foundation_allowlist contains the payer
    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

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

    // 2. Prepare the GlobalConfig PDA account meta
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);

    // 3. Build an invalid SetGlobalConfigArgs: local_asn = 0 violates GlobalConfig::validate
    let invalid_args = SetGlobalConfigArgs {
        local_asn: 0, // invalid according to validate()
        remote_asn: 65001,
        device_tunnel_block: "10.0.0.0/24".parse::<NetworkV4>().unwrap(),
        user_tunnel_block: "10.0.0.0/24".parse::<NetworkV4>().unwrap(),
        multicastgroup_block: "224.0.0.0/4".parse::<NetworkV4>().unwrap(),
        next_bgp_community: None,
    };

    let instruction = DoubleZeroInstruction::SetGlobalConfig(invalid_args);

    // 4. Try to execute the transaction and asserts it fails due to validate()
    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        instruction,
        vec![
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(
        result.is_err(),
        "SetGlobalConfig with invalid ASN should fail"
    );
}
