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
use std::{net::Ipv4Addr, str::FromStr};

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
        public_ip: [1, 2, 3, 4].into(),
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

/// A device account as the program before agent_ip wrote it: `CreateDevice`
/// run by the serviceability program at main 1e59cc751 (this PR's base), in a
/// `ProgramTest` set up as `device_test.rs` does, with code `dz1`, public IP
/// 1.2.3.4, dz_prefixes 110.1.0.0/23 and mgmt VRF `mgmt`. The program funded it
/// with exactly the rent-exempt minimum for its 240 bytes.
const DEVICE_FROM_PREVIOUS_PROGRAM_HEX: &[&str] = &[
    "05bb429b89b0112fddc16c30e87c4472e7c956c670664058863b2b80457da672b7040000000000000000000000000000",
    "00ff90a3241648865fa0ac7223851f359d78443e218e1df9b4b5fd48ab3e954aca0d93aa0d6738aeb9bed0a470a215f4",
    "3e8f8f7f98d05d112a7141bbad915012689c00010203040103000000647a31010000006e010000170000000000000000",
    "00000000000000000000000000000000000000000000000076896f8ff314caf286fb75d77c31f09862e9067a3ac38865",
    "c5904b70947fb146040000006d676d740000000000000000000000000301000000000000000000000000000000000000",
];
const PREVIOUS_PROGRAM_ID: &str = "11157t3sqMV725NVRLrVQbAu98Jjfk1uCKehJnXXQs";
const PREVIOUS_DEVICE_PDA: &str = "2ZvemYQrw8yTPk9oRezpHVGzVJTpkJxxn8RRnyjpPWL8";

#[tokio::test]
async fn test_device_created_by_previous_program_grows_on_next_write() {
    let old_data: Vec<u8> = DEVICE_FROM_PREVIOUS_PROGRAM_HEX
        .concat()
        .as_bytes()
        .chunks(2)
        .map(|b| u8::from_str_radix(std::str::from_utf8(b).unwrap(), 16).unwrap())
        .collect();
    let old_len = old_data.len();
    assert_eq!(old_len, 240);

    // The current reader takes the old account as having no agent_ip, and the
    // current writer lays out the same bytes with agent_ip appended.
    let device = Device::try_from(&old_data[..]).unwrap();
    assert_eq!(device.agent_ip, Ipv4Addr::UNSPECIFIED);
    assert_eq!(device.code, "dz1");
    let new_data = borsh::to_vec(&device).unwrap();
    assert_eq!(new_data.len(), old_len + 4);
    assert_eq!(&new_data[..old_len], &old_data[..]);
    assert_eq!(&new_data[old_len..], &[0, 0, 0, 0]);

    let program_id = Pubkey::from_str(PREVIOUS_PROGRAM_ID).unwrap();
    let device_pubkey = Pubkey::from_str(PREVIOUS_DEVICE_PDA).unwrap();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );
    let payer = test_payer();
    let (globalstate_pubkey, gs_bump) = get_globalstate_pda(&program_id);
    program_test.add_account(
        globalstate_pubkey,
        Account {
            lamports: 1_000_000_000,
            data: borsh::to_vec(&GlobalState {
                account_type: AccountType::GlobalState,
                bump_seed: gs_bump,
                account_index: device.index,
                foundation_allowlist: vec![payer.pubkey()],
                ..Default::default()
            })
            .unwrap(),
            owner: program_id,
            ..Account::default()
        },
    );
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
    assert_eq!(
        Device::try_from(&account.data[..]).unwrap(),
        Device {
            device_health: DeviceHealth::ReadyForUsers,
            ..device
        }
    );
}
