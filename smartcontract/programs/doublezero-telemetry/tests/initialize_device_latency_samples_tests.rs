use borsh::BorshSerialize;
use doublezero_program_common::types::{NetworkV4, NetworkV4List};
use doublezero_serviceability::{
    processors::{
        device::create::DeviceCreateArgs, exchange::create::ExchangeCreateArgs,
        link::create::LinkCreateArgs, location::create::LocationCreateArgs,
    },
    state::{
        accounttype::AccountType,
        device::{Device, DeviceDesiredStatus, DeviceHealth, DeviceStatus, DeviceType},
        link::{Link, LinkDesiredStatus, LinkHealth, LinkLinkType, LinkStatus},
    },
};
use doublezero_telemetry::{
    error::TelemetryError, instructions::TelemetryInstruction,
    pda::derive_device_latency_samples_pda,
    processors::telemetry::initialize_device_latency_samples::InitializeDeviceLatencySamplesArgs,
    state::device_latency_samples::DEVICE_LATENCY_SAMPLES_HEADER_SIZE,
};
use solana_program_test::*;
use solana_sdk::{
    account::Account,
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::Transaction,
};
use std::net::Ipv4Addr;

mod test_helpers;

use test_helpers::*;

const EXPECTED_LAMPORTS_USED_FOR_ACCOUNT_CREATION: u64 = 3319920;

#[tokio::test]
async fn test_initialize_device_latency_samples_success_active_devices_and_link() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Seed ledger with two linked devices, and a funded origin device agent.
    let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Execute initialize latency samples transaction.
    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &origin_device_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1u64,
            5_000_000,
        )
        .await
        .unwrap();

    // Verify account creation and data.
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(account.data.len(), DEVICE_LATENCY_SAMPLES_HEADER_SIZE);
    assert_eq!(
        account.lamports,
        EXPECTED_LAMPORTS_USED_FOR_ACCOUNT_CREATION
    );
}

#[tokio::test]
async fn test_initialize_device_latency_samples_already_with_lamports() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Seed ledger with two linked devices, and a funded origin device agent.
    let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();
    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    let epoch = 1;

    // Derive the samples PDA first so we can transfer lamports to it.
    let (latency_samples_pda, _) = derive_device_latency_samples_pda(
        &ledger.telemetry.program_id,
        &origin_device_pk,
        &target_device_pk,
        &link_pk,
        epoch,
    );

    // Transfer just enough for zero-byte rent exemption.
    ledger
        .fund_account(&latency_samples_pda, 6_960 * 128)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Execute initialize latency samples transaction.
    ledger
        .telemetry
        .initialize_device_latency_samples(
            &origin_device_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            epoch,
            5_000_000,
        )
        .await
        .unwrap();

    // Verify account creation and data.
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(account.data.len(), DEVICE_LATENCY_SAMPLES_HEADER_SIZE);
    assert_eq!(
        account.lamports,
        EXPECTED_LAMPORTS_USED_FOR_ACCOUNT_CREATION
    );
}

#[tokio::test]
async fn test_initialize_device_latency_samples_success_suspended_origin_device() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Seed ledger with two linked devices, and a funded origin device agent.
    let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    // Drain the origin device.
    ledger
        .serviceability
        .softdrained_device(contributor_pk, origin_device_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Check that the origin device has desired_status Drained.
    // Note: check_status_transition is a no-op (waiting for health oracle),
    // so status remains Activated but desired_status is Drained.
    let device = ledger
        .serviceability
        .get_device(origin_device_pk)
        .await
        .unwrap();
    assert_eq!(device.desired_status, DeviceDesiredStatus::Drained);

    // Execute initialize latency samples transaction.
    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &origin_device_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1u64,
            5_000_000,
        )
        .await
        .unwrap();

    // Verify account creation and data.
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(account.data.len(), DEVICE_LATENCY_SAMPLES_HEADER_SIZE);
    assert_eq!(
        account.lamports,
        EXPECTED_LAMPORTS_USED_FOR_ACCOUNT_CREATION
    );
}

#[tokio::test]
async fn test_initialize_device_latency_samples_success_suspended_target_device() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Seed ledger with two linked devices, and a funded origin device agent.
    let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    // Drain the target device.
    ledger
        .serviceability
        .softdrained_device(contributor_pk, target_device_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Check that the target device has desired_status Drained.
    let device = ledger
        .serviceability
        .get_device(target_device_pk)
        .await
        .unwrap();
    assert_eq!(device.desired_status, DeviceDesiredStatus::Drained);

    // Execute initialize latency samples transaction.
    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &origin_device_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1u64,
            5_000_000,
        )
        .await
        .unwrap();

    // Verify account creation and data.
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(account.data.len(), DEVICE_LATENCY_SAMPLES_HEADER_SIZE);
    assert_eq!(
        account.lamports,
        EXPECTED_LAMPORTS_USED_FOR_ACCOUNT_CREATION
    );
}

#[tokio::test]
async fn test_initialize_device_latency_samples_success_suspended_link() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Seed ledger with two linked devices, and a funded origin device agent.
    let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    // Drain the link.
    ledger
        .serviceability
        .soft_drain_link(contributor_pk, link_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Check that the link has desired_status SoftDrained.
    let link = ledger.serviceability.get_link(link_pk).await.unwrap();
    assert_eq!(link.desired_status, LinkDesiredStatus::SoftDrained);

    // Execute initialize latency samples transaction.
    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &origin_device_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1u64,
            5_000_000,
        )
        .await
        .unwrap();

    // Verify account creation and data.
    let account = ledger
        .get_account(latency_samples_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, ledger.telemetry.program_id);
    assert_eq!(account.data.len(), DEVICE_LATENCY_SAMPLES_HEADER_SIZE);
    assert_eq!(
        account.lamports,
        EXPECTED_LAMPORTS_USED_FOR_ACCOUNT_CREATION
    );
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_unauthorized_agent() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Seed ledger with two linked devices, and a funded origin device agent.
    let (_origin_device_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();
    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Create and fund an unauthorized agent keypair.
    let unauthorized_agent = Keypair::new();
    let unauthorized_agent_pk = unauthorized_agent.pubkey();
    ledger
        .fund_account(&unauthorized_agent_pk, 10_000_000_000)
        .await
        .unwrap();

    // Execute initialize latency samples transaction with unauthorized agent.
    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &unauthorized_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            1u64,
            5_000_000,
        )
        .await;
    assert_telemetry_error(result, TelemetryError::UnauthorizedAgent);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_agent_not_signer() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    // Seed with two linked devices and a valid agent.
    let (origin_device_agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Create PDA manually.
    let (latency_samples_pda, _) = derive_device_latency_samples_pda(
        &ledger.telemetry.program_id,
        &origin_device_pk,
        &target_device_pk,
        &link_pk,
        1,
    );

    // Construct instruction manually with agent NOT a signer.
    let args = InitializeDeviceLatencySamplesArgs {
        epoch: 1,
        sampling_interval_microseconds: 5_000_000,
    };

    let instruction = TelemetryInstruction::InitializeDeviceLatencySamples(args.clone());
    let data = instruction.pack().unwrap();

    let accounts = vec![
        AccountMeta::new(latency_samples_pda, false),
        AccountMeta::new_readonly(origin_device_agent.pubkey(), false), // Not signer
        AccountMeta::new_readonly(origin_device_pk, false),
        AccountMeta::new_readonly(target_device_pk, false),
        AccountMeta::new_readonly(link_pk, false),
        AccountMeta::new_readonly(solana_system_interface::program::ID, false),
        AccountMeta::new_readonly(ledger.serviceability.program_id, false),
    ];

    let (banks_client, payer, recent_blockhash) = {
        let ctx = ledger.context.lock().unwrap();
        (
            ctx.banks_client.clone(),
            ctx.payer.insecure_clone(),
            ctx.recent_blockhash,
        )
    };

    let mut transaction = Transaction::new_with_payer(
        &[solana_sdk::instruction::Instruction {
            program_id: ledger.telemetry.program_id,
            accounts,
            data,
        }],
        Some(&payer.pubkey()),
    );

    transaction.sign(&[&payer], recent_blockhash);

    let result = banks_client.process_transaction(transaction).await;
    assert_banksclient_error(result, InstructionError::MissingRequiredSignature);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_origin_device_wrong_owner() {
    let agent = Keypair::new();
    let fake_origin_device_pk = Pubkey::new_unique();
    let target_device_pk = Pubkey::new_unique(); // doesn’t matter, we won’t get that far
    let link_pk = Pubkey::new_unique(); // same

    let fake_origin_device = Device {
        index: 0,
        bump_seed: 0,
        reference_count: 0,
        account_type: AccountType::Device,
        code: "invalid".to_string(),
        owner: agent.pubkey(),
        contributor_pk: Pubkey::new_unique(),
        exchange_pk: Pubkey::new_unique(),
        device_type: DeviceType::Hybrid,
        public_ip: Ipv4Addr::UNSPECIFIED,
        status: DeviceStatus::Activated,
        metrics_publisher_pk: agent.pubkey(),
        location_pk: Pubkey::new_unique(),
        dz_prefixes: NetworkV4List::default(),
        mgmt_vrf: "default".to_string(),
        interfaces: vec![],
        users_count: 0,
        max_users: 0,
        device_health: DeviceHealth::Pending,
        desired_status: DeviceDesiredStatus::Pending,
        unicast_users_count: 0,
        multicast_users_count: 0,
        max_unicast_users: 0,
        max_multicast_users: 0,
        reserved_seats: 0,
    };

    let mut device_data = Vec::new();
    fake_origin_device.serialize(&mut device_data).unwrap();

    let fake_account = Account {
        lamports: 1_000_000,
        data: device_data,
        owner: Pubkey::new_unique(), // WRONG owner
        executable: false,
        rent_epoch: 0,
    };

    let mut ledger =
        LedgerHelper::new_with_preloaded_accounts(vec![(fake_origin_device_pk, fake_account)])
            .await
            .unwrap();

    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            fake_origin_device_pk,
            target_device_pk,
            link_pk,
            42,
            5_000_000,
        )
        .await;
    assert_banksclient_error(result, InstructionError::IncorrectProgramId);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_target_device_wrong_owner() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, _real_target_device, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Inject a fake target device account with wrong owner
    let fake_target_device_pk = Pubkey::new_unique();
    let wrong_owner = Pubkey::new_unique();

    let fake_target_device = Device {
        status: DeviceStatus::Activated,
        metrics_publisher_pk: Pubkey::new_unique(), // doesn't matter for Z
        location_pk: Pubkey::new_unique(),
        dz_prefixes: NetworkV4List::default(),
        account_type: AccountType::Device,
        owner: wrong_owner,
        index: 0,
        bump_seed: 0,
        reference_count: 0,
        contributor_pk: Pubkey::new_unique(),
        exchange_pk: Pubkey::new_unique(),
        device_type: DeviceType::Hybrid,
        public_ip: Ipv4Addr::UNSPECIFIED,
        code: "invalid".to_string(),
        mgmt_vrf: "default".to_string(),
        interfaces: vec![],
        users_count: 0,
        max_users: 0,
        device_health: DeviceHealth::Pending,
        desired_status: DeviceDesiredStatus::Pending,
        unicast_users_count: 0,
        multicast_users_count: 0,
        max_unicast_users: 0,
        max_multicast_users: 0,
        reserved_seats: 0,
    };

    let mut data = Vec::new();
    fake_target_device.serialize(&mut data).unwrap();

    let fake_account = solana_sdk::account::Account {
        lamports: 1_000_000,
        data,
        owner: wrong_owner,
        executable: false,
        rent_epoch: 0,
    };

    let mut ledger =
        LedgerHelper::new_with_preloaded_accounts(vec![(fake_target_device_pk, fake_account)])
            .await
            .unwrap();

    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            fake_target_device_pk,
            link_pk,
            88,
            5_000_000,
        )
        .await;

    assert_banksclient_error(result, InstructionError::IncorrectProgramId);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_link_wrong_owner() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, _real_link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    // Inject a fake Link account with wrong owner
    let fake_link_pk = Pubkey::new_unique();
    let wrong_owner = Pubkey::new_unique();

    let fake_link = Link {
        status: LinkStatus::Activated,
        contributor_pk: Pubkey::default(),
        side_a_pk: origin_device_pk,
        side_z_pk: target_device_pk,
        account_type: AccountType::Link,
        owner: wrong_owner,
        index: 0,
        bump_seed: 0,
        code: "invalid".to_string(),
        bandwidth: 10_000_000_000,
        delay_ns: 10000,
        jitter_ns: 10000,
        delay_override_ns: 0,
        link_type: LinkLinkType::WAN,
        mtu: 0,
        tunnel_id: 0,
        tunnel_net: NetworkV4::default(),
        side_a_iface_name: "Ethernet0".to_string(),
        side_z_iface_name: "Ethernet1".to_string(),
        link_health: LinkHealth::ReadyForService,
        desired_status: doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
    };

    let mut data = Vec::new();
    fake_link.serialize(&mut data).unwrap();

    let fake_account = solana_sdk::account::Account {
        lamports: 1_000_000,
        data,
        owner: wrong_owner,
        executable: false,
        rent_epoch: 0,
    };

    let mut ledger = LedgerHelper::new_with_preloaded_accounts(vec![(fake_link_pk, fake_account)])
        .await
        .unwrap();

    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();
    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            fake_link_pk,
            77,
            5_000_000,
        )
        .await;

    assert_banksclient_error(result, InstructionError::IncorrectProgramId);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_origin_device_not_activated() {
    let mut ledger = LedgerHelper::new().await.unwrap();
    let payer = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();

    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer)
        .await
        .unwrap();

    let location_pk = ledger
        .serviceability
        .create_location(LocationCreateArgs {
            code: "LOC1".to_string(),
            name: "Test Location".to_string(),
            country: "US".to_string(),
            loc_id: 1,
            ..LocationCreateArgs::default()
        })
        .await
        .unwrap();

    let exchange_pk = ledger
        .serviceability
        .create_exchange(ExchangeCreateArgs {
            code: "EX1".to_string(),
            name: "Test Exchange".to_string(),
            reserved: 0,
            ..ExchangeCreateArgs::default()
        })
        .await
        .unwrap();

    let agent = Keypair::new();
    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    // Origin device: not activated
    let origin_device_pk = ledger
        .serviceability
        .create_device(
            DeviceCreateArgs {
                code: "OriginDevice".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 1].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(origin_device_pk, contributor_pk, "Ethernet0".to_string())
        .await
        .unwrap();

    // Target device: activated
    let target_device_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "TargetDevice".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 1].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(target_device_pk, contributor_pk, "Ethernet1".to_string())
        .await
        .unwrap();

    // Link: between origin device and target device
    let link_pk = ledger
        .serviceability
        .create_and_activate_link(
            LinkCreateArgs {
                code: "LINK1".to_string(),
                link_type: LinkLinkType::WAN,
                bandwidth: 10_000_000_000,
                mtu: 1500,
                delay_ns: 1000000,
                jitter_ns: 100000,
                side_a_iface_name: "Ethernet0".to_string(),
                side_z_iface_name: Some("Ethernet1".to_string()),
                desired_status: Some(LinkDesiredStatus::Activated),
            },
            contributor_pk,
            origin_device_pk,
            target_device_pk,
            1,
            "10.1.1.0/30".parse().unwrap(),
        )
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            66,
            5_000_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::DeviceNotActivated);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_target_device_not_activated() {
    let mut ledger = LedgerHelper::new().await.unwrap();
    let payer = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();

    let location_pk = ledger
        .serviceability
        .create_location(LocationCreateArgs {
            code: "LOC1".to_string(),
            name: "Test Location".to_string(),
            country: "US".to_string(),
            loc_id: 1,
            ..LocationCreateArgs::default()
        })
        .await
        .unwrap();

    let exchange_pk = ledger
        .serviceability
        .create_exchange(ExchangeCreateArgs {
            code: "EX1".to_string(),
            name: "Test Exchange".to_string(),
            reserved: 0,
            ..ExchangeCreateArgs::default()
        })
        .await
        .unwrap();

    let agent = Keypair::new();
    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer)
        .await
        .unwrap();

    // Origin device: activated
    let origin_device_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "OriginDevice".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 1].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(origin_device_pk, contributor_pk, "Ethernet0".to_string())
        .await
        .unwrap();

    // Target device: not activated
    let target_device_pk = ledger
        .serviceability
        .create_device(
            DeviceCreateArgs {
                code: "TargetDevice".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 1].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(target_device_pk, contributor_pk, "Ethernet1".to_string())
        .await
        .unwrap();

    // Link between origin device and target device
    let link_pk = ledger
        .serviceability
        .create_and_activate_link(
            LinkCreateArgs {
                code: "LINK1".to_string(),
                link_type: LinkLinkType::WAN,
                bandwidth: 10_000_000_000,
                mtu: 1500,
                delay_ns: 1000000,
                jitter_ns: 100000,
                side_a_iface_name: "Ethernet0".to_string(),
                side_z_iface_name: Some("Ethernet1".to_string()),
                desired_status: Some(LinkDesiredStatus::Activated),
            },
            contributor_pk,
            origin_device_pk,
            target_device_pk,
            1,
            "10.1.1.0/30".parse().unwrap(),
        )
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            66,
            5_000_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::DeviceNotActivated);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_success_provisioning_link() {
    let mut ledger = LedgerHelper::new().await.unwrap();
    let payer = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();

    let location_pk = ledger
        .serviceability
        .create_location(LocationCreateArgs {
            code: "LOC1".to_string(),
            name: "Test Location".to_string(),
            country: "US".to_string(),
            loc_id: 1,
            ..LocationCreateArgs::default()
        })
        .await
        .unwrap();

    let exchange_pk = ledger
        .serviceability
        .create_exchange(ExchangeCreateArgs {
            code: "EX1".to_string(),
            name: "Test Exchange".to_string(),
            reserved: 0,
            ..ExchangeCreateArgs::default()
        })
        .await
        .unwrap();

    let agent = Keypair::new();
    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer)
        .await
        .unwrap();

    let origin_device_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "OriginDevice".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 1].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(origin_device_pk, contributor_pk, "Ethernet0".to_string())
        .await
        .unwrap();

    let target_device_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "TargetDevice".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 2].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(target_device_pk, contributor_pk, "Ethernet1".to_string())
        .await
        .unwrap();

    // Create and activate link
    let link_pk = ledger
        .serviceability
        .create_and_activate_link(
            LinkCreateArgs {
                code: "LINK1".to_string(),
                link_type: LinkLinkType::WAN,
                bandwidth: 10_000_000_000,
                mtu: 1500,
                delay_ns: 1000000,
                jitter_ns: 100000,
                side_a_iface_name: "Ethernet0".to_string(),
                side_z_iface_name: Some("Ethernet1".to_string()),
                desired_status: Some(LinkDesiredStatus::Activated),
            },
            contributor_pk,
            origin_device_pk,
            target_device_pk,
            1,
            "10.1.1.0/30".parse().unwrap(),
        )
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            66,
            5_000_000,
        )
        .await;

    // Provisioning links now allow telemetry for burn-in testing
    assert!(result.is_ok());
}

#[tokio::test]
async fn test_initialize_device_latency_samples_success_soft_drained_link() {
    let mut ledger = LedgerHelper::new().await.unwrap();
    let payer = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();

    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    // Soft drain the link
    ledger
        .serviceability
        .soft_drain_link(contributor_pk, link_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            77,
            5_000_000,
        )
        .await;

    assert!(result.is_ok());
}

#[tokio::test]
async fn test_initialize_device_latency_samples_success_hard_drained_link() {
    let mut ledger = LedgerHelper::new().await.unwrap();
    let payer = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();

    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    // Hard drain the link
    ledger
        .serviceability
        .hard_drain_link(contributor_pk, link_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            88,
            5_000_000,
        )
        .await;

    assert!(result.is_ok());
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_link_wrong_devices() {
    let mut ledger = LedgerHelper::new().await.unwrap();
    let payer = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();

    let location_pk = ledger
        .serviceability
        .create_location(LocationCreateArgs {
            code: "LOC1".to_string(),
            name: "Test Location".to_string(),
            country: "US".to_string(),
            loc_id: 1,
            ..LocationCreateArgs::default()
        })
        .await
        .unwrap();

    let exchange_pk = ledger
        .serviceability
        .create_exchange(ExchangeCreateArgs {
            code: "EX1".to_string(),
            name: "Test Exchange".to_string(),
            reserved: 0,
            ..ExchangeCreateArgs::default()
        })
        .await
        .unwrap();

    let agent = Keypair::new();
    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer)
        .await
        .unwrap();
    // Origin device and target device: activated
    let origin_device_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "OriginDevice".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 1].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    let target_device_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "TargetDevice".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 2].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    // Other devices for the link
    let device_x_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "DeviceX".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 3].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(device_x_pk, contributor_pk, "Ethernet0".to_string())
        .await
        .unwrap();

    let device_y_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "DeviceY".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 4].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(device_y_pk, contributor_pk, "Ethernet1".to_string())
        .await
        .unwrap();

    // Link between X and Y — not origin device and target device
    let link_pk = ledger
        .serviceability
        .create_and_activate_link(
            LinkCreateArgs {
                code: "LINK1".to_string(),
                link_type: LinkLinkType::WAN,
                bandwidth: 10_000_000_000,
                mtu: 1500,
                delay_ns: 1000000,
                jitter_ns: 100000,
                side_a_iface_name: "Ethernet0".to_string(),
                side_z_iface_name: Some("Ethernet1".to_string()),
                desired_status: Some(LinkDesiredStatus::Activated),
            },
            contributor_pk,
            device_x_pk,
            device_y_pk,
            1,
            "10.1.1.0/30".parse().unwrap(),
        )
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            55,
            5_000_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::InvalidLink);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_succeeds_with_reversed_link_sides() {
    let mut ledger = LedgerHelper::new().await.unwrap();
    let payer = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();

    let location_pk = ledger
        .serviceability
        .create_location(LocationCreateArgs {
            code: "LOC1".into(),
            name: "Location".into(),
            country: "US".into(),
            loc_id: 1,
            ..LocationCreateArgs::default()
        })
        .await
        .unwrap();

    let exchange_pk = ledger
        .serviceability
        .create_exchange(ExchangeCreateArgs {
            code: "EX1".into(),
            name: "Exchange".into(),
            reserved: 0,
            ..ExchangeCreateArgs::default()
        })
        .await
        .unwrap();

    let agent = Keypair::new();
    ledger
        .fund_account(&agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer)
        .await
        .unwrap();

    let origin_device_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "OriginDevice".into(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 1].into(),
                dz_prefixes: vec!["109.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(origin_device_pk, contributor_pk, "Ethernet0".to_string())
        .await
        .unwrap();

    let target_device_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "TargetDevice".into(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 2].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(target_device_pk, contributor_pk, "Ethernet1".to_string())
        .await
        .unwrap();

    // link with target_device on side_a, origin_device on side_z
    let link_pk = ledger
        .serviceability
        .create_and_activate_link(
            LinkCreateArgs {
                code: "LINK1".into(),
                link_type: LinkLinkType::WAN,
                bandwidth: 10_000_000_000,
                mtu: 1500,
                delay_ns: 1000000,
                jitter_ns: 100000,
                side_a_iface_name: "Ethernet1".to_string(),
                side_z_iface_name: Some("Ethernet0".to_string()),
                desired_status: Some(LinkDesiredStatus::Activated),
            },
            contributor_pk,
            target_device_pk,
            origin_device_pk,
            1,
            "192.168.0.0/24".parse().unwrap(),
        )
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            42,
            5_000_000,
        )
        .await;

    assert!(result.is_ok());
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_account_already_exists() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    // First call: succeed and create the account
    let latency_samples_pda = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            999,
            5_000_000,
        )
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Second call: explicitly pass the same latency_samples_pda as the account
    let result = ledger
        .telemetry
        .initialize_device_latency_samples_with_pda(
            &agent,
            latency_samples_pda,
            origin_device_pk,
            target_device_pk,
            link_pk,
            999,
            5_000_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::AccountAlreadyExists);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_invalid_pda() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    // Derive correct PDA (but we won't use it)
    let (_correct_pda, _bump) = derive_device_latency_samples_pda(
        &ledger.telemetry.program_id,
        &origin_device_pk,
        &target_device_pk,
        &link_pk,
        42,
    );

    // Use a wrong/fake PDA
    let fake_pda = Pubkey::new_unique();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples_with_pda(
            &agent,
            fake_pda,
            origin_device_pk,
            target_device_pk,
            link_pk,
            42,
            5_000_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::InvalidPDA);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_zero_sampling_interval() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            123,
            0,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::InvalidSamplingInterval);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_same_origin_device_and_target_device() {
    let mut ledger = LedgerHelper::new().await.unwrap();

    let payer_pubkey = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();
    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer_pubkey)
        .await
        .unwrap();

    let (agent, origin_device_pk, _target_device_pk, link_pk) = ledger
        .seed_with_two_linked_devices(contributor_pk)
        .await
        .unwrap();

    ledger.wait_for_new_blockhash().await.unwrap();

    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &agent,
            origin_device_pk,
            origin_device_pk,
            link_pk,
            123,
            100_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::InvalidLink);
}

#[tokio::test]
async fn test_initialize_device_latency_samples_fail_agent_not_owner_of_origin_device() {
    let mut ledger = LedgerHelper::new().await.unwrap();
    let payer = ledger
        .context
        .lock()
        .unwrap()
        .payer
        .insecure_clone()
        .pubkey();

    // Create agent that owns origin device
    let owner_agent = Keypair::new();
    ledger
        .fund_account(&owner_agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    // Create a separate, unauthorized agent
    let unauthorized_agent = Keypair::new();
    ledger
        .fund_account(&unauthorized_agent.pubkey(), 10_000_000_000)
        .await
        .unwrap();

    let location_pk = ledger
        .serviceability
        .create_location(LocationCreateArgs {
            code: "LOC1".to_string(),
            name: "Loc".to_string(),
            country: "CA".to_string(),
            loc_id: 1,
            ..LocationCreateArgs::default()
        })
        .await
        .unwrap();

    let exchange_pk = ledger
        .serviceability
        .create_exchange(ExchangeCreateArgs {
            code: "EX".to_string(),
            name: "Exchange".to_string(),
            reserved: 0,
            ..ExchangeCreateArgs::default()
        })
        .await
        .unwrap();

    let contributor_pk = ledger
        .serviceability
        .create_contributor("CONTRIB".to_string(), payer)
        .await
        .unwrap();

    // Origin device: activated, owned by owner_agent
    let origin_device_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "A".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 1].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: owner_agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(origin_device_pk, contributor_pk, "Ethernet0".to_string())
        .await
        .unwrap();

    // Target device: also valid
    let target_device_pk = ledger
        .serviceability
        .create_and_activate_device(
            DeviceCreateArgs {
                code: "Z".to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: [100, 0, 0, 2].into(),
                dz_prefixes: vec!["108.0.0.0/24".parse().unwrap()].into(),
                metrics_publisher_pk: unauthorized_agent.pubkey(),
                ..DeviceCreateArgs::default()
            },
            contributor_pk,
            location_pk,
            exchange_pk,
        )
        .await
        .unwrap();

    ledger
        .serviceability
        .create_interface(target_device_pk, contributor_pk, "Ethernet1".to_string())
        .await
        .unwrap();

    let link_pk = ledger
        .serviceability
        .create_and_activate_link(
            LinkCreateArgs {
                code: "LNK".to_string(),
                link_type: LinkLinkType::WAN,
                bandwidth: 10_000_000_000,
                mtu: 4500,
                delay_ns: 1000000,
                jitter_ns: 100000,
                side_a_iface_name: "Ethernet0".to_string(),
                side_z_iface_name: Some("Ethernet1".to_string()),
                desired_status: Some(LinkDesiredStatus::Activated),
            },
            contributor_pk,
            origin_device_pk,
            target_device_pk,
            1,
            "10.0.0.0/24".parse().unwrap(),
        )
        .await
        .unwrap();

    // Wait for a new blockhash before moving on.
    ledger.wait_for_new_blockhash().await.unwrap();

    // Attempt with the unauthorized agent
    let result = ledger
        .telemetry
        .initialize_device_latency_samples(
            &unauthorized_agent,
            origin_device_pk,
            target_device_pk,
            link_pk,
            66,
            5_000_000,
        )
        .await;

    assert_telemetry_error(result, TelemetryError::UnauthorizedAgent);
}
