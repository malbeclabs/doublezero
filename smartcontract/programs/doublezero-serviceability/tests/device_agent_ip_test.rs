//! Integration tests for agent_ip in CreateDevice and UpdateDevice: setting it,
//! changing it, clearing it, and refusing any address that is not public IPv4.

use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::device::{create::DeviceCreateArgs, update::DeviceUpdateArgs},
    resource::ResourceType,
    state::device::*,
};
use solana_program_test::*;
use solana_sdk::{
    hash::Hash,
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    signature::Keypair,
    transaction::TransactionError,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

const INVALID_AGENT_IP: u32 = 134;

/// Addresses that are not public IPv4: unspecified "this network", private,
/// CGNAT, loopback, link-local, IETF, documentation, multicast, reserved and
/// broadcast.
const NOT_PUBLIC: [Ipv4Addr; 13] = [
    Ipv4Addr::new(0, 1, 2, 3),
    Ipv4Addr::new(10, 0, 0, 1),
    Ipv4Addr::new(100, 64, 0, 1),
    Ipv4Addr::new(127, 0, 0, 1),
    Ipv4Addr::new(169, 254, 0, 1),
    Ipv4Addr::new(172, 16, 0, 1),
    Ipv4Addr::new(192, 0, 0, 1),
    Ipv4Addr::new(192, 0, 2, 1),
    Ipv4Addr::new(192, 168, 1, 1),
    Ipv4Addr::new(198, 51, 100, 1),
    Ipv4Addr::new(224, 0, 0, 1),
    Ipv4Addr::new(240, 0, 0, 1),
    Ipv4Addr::BROADCAST,
];

const AGENT_IP: Ipv4Addr = Ipv4Addr::new(5, 6, 7, 8);
const OTHER_AGENT_IP: Ipv4Addr = Ipv4Addr::new(2, 3, 4, 5);

fn assert_custom_error(result: Result<(), BanksClientError>, expected: u32, context: &str) {
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(code),
        ))) if code == expected => {}
        _ => panic!("{context}: expected Custom({expected}), got {result:?}"),
    }
}

struct Setup {
    banks_client: BanksClient,
    payer: Keypair,
    program_id: Pubkey,
    recent_blockhash: Hash,
    globalstate_pubkey: Pubkey,
    contributor_pubkey: Pubkey,
    device_pubkey: Pubkey,
    create_accounts: Vec<AccountMeta>,
}

async fn setup() -> Setup {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (location_pubkey, exchange_pubkey, contributor_pubkey) = setup_device_prerequisites(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    Setup {
        banks_client,
        payer,
        program_id,
        recent_blockhash,
        globalstate_pubkey,
        contributor_pubkey,
        device_pubkey,
        create_accounts: vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
    }
}

fn create_args(agent_ip: Ipv4Addr) -> DoubleZeroInstruction {
    DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
        code: "dz1".to_string(),
        device_type: DeviceType::Hybrid,
        public_ip: [1, 2, 3, 4].into(),
        dz_prefixes: "110.1.0.0/23".parse().unwrap(),
        metrics_publisher_pk: Pubkey::default(),
        mgmt_vrf: "mgmt".to_string(),
        desired_status: Some(DeviceDesiredStatus::Activated),
        resource_count: 2,
        agent_ip,
    })
}

fn update_args(agent_ip: Option<Ipv4Addr>) -> DoubleZeroInstruction {
    DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
        agent_ip,
        ..DeviceUpdateArgs::default()
    })
}

impl Setup {
    fn update_accounts(&self) -> Vec<AccountMeta> {
        vec![
            AccountMeta::new(self.device_pubkey, false),
            AccountMeta::new(self.contributor_pubkey, false),
            AccountMeta::new(self.globalstate_pubkey, false),
        ]
    }

    async fn create(&mut self, agent_ip: Ipv4Addr) {
        execute_transaction(
            &mut self.banks_client,
            self.recent_blockhash,
            self.program_id,
            create_args(agent_ip),
            self.create_accounts.clone(),
            &self.payer,
        )
        .await;
    }

    async fn update(&mut self, args: DoubleZeroInstruction) {
        let accounts = self.update_accounts();
        execute_transaction(
            &mut self.banks_client,
            self.recent_blockhash,
            self.program_id,
            args,
            accounts,
            &self.payer,
        )
        .await;
    }

    async fn device(&mut self) -> Device {
        get_device(&mut self.banks_client, self.device_pubkey)
            .await
            .expect("Device not found")
    }
}

#[tokio::test]
async fn test_device_agent_ip_create_refuses_non_public() {
    let mut s = setup().await;

    for agent_ip in NOT_PUBLIC {
        let result = execute_transaction_expect_failure(
            &mut s.banks_client,
            s.recent_blockhash,
            s.program_id,
            create_args(agent_ip),
            s.create_accounts.clone(),
            &s.payer,
        )
        .await;
        assert_custom_error(
            result,
            INVALID_AGENT_IP,
            &format!("CreateDevice with agent_ip {agent_ip}"),
        );
    }
    let device_account = s.banks_client.get_account(s.device_pubkey).await.unwrap();
    assert!(device_account.is_none(), "no device may be created");
}

#[tokio::test]
async fn test_device_agent_ip_create_with_agent_ip() {
    let mut s = setup().await;
    s.create(AGENT_IP).await;
    assert_eq!(s.device().await.agent_ip, AGENT_IP);
}

#[tokio::test]
async fn test_device_agent_ip_create_without_agent_ip() {
    let mut s = setup().await;
    s.create(Ipv4Addr::UNSPECIFIED).await;
    assert_eq!(s.device().await.agent_ip, Ipv4Addr::UNSPECIFIED);
}

#[tokio::test]
async fn test_device_agent_ip_update_add_change_clear() {
    let mut s = setup().await;
    s.create(Ipv4Addr::UNSPECIFIED).await;
    let created = s.device().await;

    // Add: the address is stored and nothing else on the device changes.
    s.update(update_args(Some(AGENT_IP))).await;
    assert_eq!(
        s.device().await,
        Device {
            agent_ip: AGENT_IP,
            ..created.clone()
        }
    );

    // Change it to another public address.
    s.update(update_args(Some(OTHER_AGENT_IP))).await;
    assert_eq!(s.device().await.agent_ip, OTHER_AGENT_IP);

    // An update that does not name agent_ip leaves it alone.
    s.update(DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
        max_users: Some(64),
        ..DeviceUpdateArgs::default()
    }))
    .await;
    assert_eq!(s.device().await.agent_ip, OTHER_AGENT_IP);

    // Clear: 0.0.0.0 removes it.
    s.update(update_args(Some(Ipv4Addr::UNSPECIFIED))).await;
    assert_eq!(s.device().await.agent_ip, Ipv4Addr::UNSPECIFIED);
}

#[tokio::test]
async fn test_device_agent_ip_update_refuses_non_public() {
    let mut s = setup().await;
    s.create(AGENT_IP).await;

    for agent_ip in NOT_PUBLIC {
        let accounts = s.update_accounts();
        let result = execute_transaction_expect_failure(
            &mut s.banks_client,
            s.recent_blockhash,
            s.program_id,
            update_args(Some(agent_ip)),
            accounts,
            &s.payer,
        )
        .await;
        assert_custom_error(
            result,
            INVALID_AGENT_IP,
            &format!("UpdateDevice with agent_ip {agent_ip}"),
        );
    }
    // A refused update leaves the stored address as it was.
    assert_eq!(s.device().await.agent_ip, AGENT_IP);
}
