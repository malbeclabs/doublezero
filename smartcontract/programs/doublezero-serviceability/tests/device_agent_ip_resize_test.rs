//! A Device account written before agent_ip existed ends at the interfaces vec.
//! Its next write by any instruction, not only UpdateDevice, must grow it by the
//! 4 bytes of agent_ip, with the payer covering the extra rent.

use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::device::sethealth::DeviceSetHealthArgs,
    state::{accounttype::AccountType, device::*, globalstate::GlobalState},
};
use solana_program_test::*;
use solana_sdk::{
    account::Account, instruction::AccountMeta, pubkey::Pubkey, rent::Rent, signer::Signer,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_device_written_before_agent_ip_grows_on_next_write() {
    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );
    let payer = test_payer();

    let (globalstate_pubkey, gs_bump) = get_globalstate_pda(&program_id);
    let (device_pubkey, dev_bump) = get_device_pda(&program_id, 1);

    let globalstate = GlobalState {
        account_type: AccountType::GlobalState,
        bump_seed: gs_bump,
        account_index: 1,
        foundation_allowlist: vec![payer.pubkey()],
        ..Default::default()
    };
    program_test.add_account(
        globalstate_pubkey,
        Account {
            lamports: 1_000_000_000,
            data: borsh::to_vec(&globalstate).unwrap(),
            owner: program_id,
            ..Account::default()
        },
    );

    let device = Device {
        account_type: AccountType::Device,
        owner: payer.pubkey(),
        index: 1,
        bump_seed: dev_bump,
        location_pk: Pubkey::new_unique(),
        exchange_pk: Pubkey::new_unique(),
        device_type: DeviceType::Hybrid,
        public_ip: [8, 8, 8, 8].into(),
        status: DeviceStatus::Activated,
        code: "dz1".to_string(),
        dz_prefixes: "110.1.0.0/23".parse().unwrap(),
        contributor_pk: Pubkey::new_unique(),
        mgmt_vrf: "mgmt".to_string(),
        device_health: DeviceHealth::Pending,
        ..Default::default()
    };
    // The account as written before agent_ip existed: the current layout minus its
    // last 4 bytes, funded only to the rent-exempt minimum for that length.
    let mut old_data = borsh::to_vec(&device).unwrap();
    old_data.truncate(old_data.len() - 4);
    let old_len = old_data.len();
    let rent = Rent::default();
    program_test.add_account(
        device_pubkey,
        Account {
            lamports: rent.minimum_balance(old_len),
            data: old_data,
            owner: program_id,
            ..Account::default()
        },
    );

    let (mut banks_client, funder, recent_blockhash) = program_test.start().await;
    transfer(&mut banks_client, &funder, &payer.pubkey(), 100_000_000).await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetDeviceHealth(DeviceSetHealthArgs {
            health: DeviceHealth::ReadyForUsers,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let account = banks_client
        .get_account(device_pubkey)
        .await
        .unwrap()
        .expect("Device not found");
    assert_eq!(account.data.len(), old_len + 4);
    assert!(account.lamports >= rent.minimum_balance(old_len + 4));

    let stored = Device::try_from(&account.data[..]).unwrap();
    assert_eq!(
        stored,
        Device {
            device_health: DeviceHealth::ReadyForUsers,
            agent_ip: Ipv4Addr::UNSPECIFIED,
            ..device
        }
    );
}
