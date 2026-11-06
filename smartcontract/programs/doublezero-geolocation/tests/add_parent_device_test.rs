//! `AddParentDevice` deserializes the whole serviceability Device on this program's stock 32 KiB
//! heap. Serviceability caps how large a Device can grow (`MAX_DEVICE_ACCOUNT_SIZE`); a device at
//! that cap must still load here. Meaningful under `cargo test-sbf`: the native build has no heap
//! limit.
// Only some of the shared helpers are used here.
#[allow(dead_code)]
mod test_helpers;

use doublezero_geolocation::{
    entrypoint::process_instruction,
    instructions::GeolocationInstruction,
    pda::{get_geo_probe_pda, get_program_config_pda},
    processors::{
        geo_probe::create::CreateGeoProbeArgs, program_config::init::InitProgramConfigArgs,
    },
    serviceability_program_id,
    state::geo_probe::GeoProbe,
};
use doublezero_serviceability::state::{
    device::{Device, DeviceStatus, MAX_DEVICE_ACCOUNT_SIZE},
    exchange::ExchangeStatus,
    interface::{Interface, InterfaceStatus, InterfaceType},
};
use solana_program_test::*;
use solana_sdk::{
    account::AccountSharedData,
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    signature::Signer,
    transaction::Transaction,
};
#[allow(deprecated)]
use solana_sdk_ids::bpf_loader_upgradeable;
use std::net::Ipv4Addr;
use test_helpers::*;

/// The largest Device serviceability lets an account grow to, built from interfaces named by
/// `name`. Short names are the worst case for the heap: they pack the most interfaces into the
/// bytes.
fn max_size_device(name: impl Fn(usize) -> String) -> Vec<u8> {
    let mut device = Device {
        code: "max-size".to_string(),
        status: DeviceStatus::Activated,
        location_pk: Pubkey::new_unique(),
        exchange_pk: Pubkey::new_unique(),
        contributor_pk: Pubkey::new_unique(),
        dz_prefixes: "110.1.0.0/23".parse().unwrap(),
        mgmt_vrf: "mgmt".to_string(),
        public_ip: [8, 8, 8, 8].into(),
        ..Default::default()
    };
    let mut data = borsh::to_vec(&device).unwrap();
    for i in 0.. {
        let mut iface = Interface {
            status: InterfaceStatus::Activated,
            name: name(i),
            interface_type: InterfaceType::Physical,
            ..Default::default()
        };
        iface.size = iface.compute_on_disk_size().unwrap();
        device.interfaces.push(iface);
        let next = borsh::to_vec(&device).unwrap();
        if next.len() > MAX_DEVICE_ACCOUNT_SIZE {
            break;
        }
        data = next;
    }
    data
}

async fn add_max_size_parent_device(name: impl Fn(usize) -> String) {
    let program_id = Pubkey::new_unique();
    let program_test = ProgramTest::new(
        "doublezero_geolocation",
        program_id,
        processor!(process_instruction),
    );
    let mut context = program_test.start_with_context().await;
    let payer = context.payer.insecure_clone();

    let (program_data_pda, _) =
        Pubkey::find_program_address(&[program_id.as_ref()], &bpf_loader_upgradeable::id());
    context.set_account(
        &program_data_pda,
        &build_program_data_account(&payer.pubkey()),
    );
    let globalstate =
        doublezero_serviceability::pda::get_globalstate_pda(&serviceability_program_id()).0;
    context.set_account(
        &globalstate,
        &create_mock_globalstate_account_shared(vec![payer.pubkey()]),
    );
    let exchange = Pubkey::new_unique();
    context.set_account(
        &exchange,
        &create_mock_exchange_account_shared(&payer.pubkey(), ExchangeStatus::Activated),
    );
    let data = max_size_device(name);
    let mut device_account =
        AccountSharedData::new(1_000_000_000, data.len(), &serviceability_program_id());
    device_account.set_data_from_slice(&data);
    let device = Pubkey::new_unique();
    context.set_account(&device, &device_account);

    let (program_config_pda, _) = get_program_config_pda(&program_id);
    let (probe_pda, _) = get_geo_probe_pda(&program_id, "probe");
    let setup = [
        Instruction::new_with_borsh(
            program_id,
            &GeolocationInstruction::InitProgramConfig(InitProgramConfigArgs {}),
            vec![
                AccountMeta::new(program_config_pda, false),
                AccountMeta::new_readonly(program_data_pda, false),
                AccountMeta::new(payer.pubkey(), true),
                AccountMeta::new_readonly(solana_sdk_ids::system_program::id(), false),
            ],
        ),
        Instruction::new_with_borsh(
            program_id,
            &GeolocationInstruction::CreateGeoProbe(CreateGeoProbeArgs {
                code: "probe".to_string(),
                public_ip: Ipv4Addr::new(8, 8, 8, 8),
                location_offset_port: 4242,
                metrics_publisher_pk: Pubkey::new_unique(),
            }),
            vec![
                AccountMeta::new(probe_pda, false),
                AccountMeta::new_readonly(exchange, false),
                AccountMeta::new_readonly(program_config_pda, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(payer.pubkey(), true),
                AccountMeta::new_readonly(solana_sdk_ids::system_program::id(), false),
            ],
        ),
    ];
    let tx = Transaction::new_signed_with_payer(
        &setup,
        Some(&payer.pubkey()),
        &[&payer],
        context.last_blockhash,
    );
    context.banks_client.process_transaction(tx).await.unwrap();

    let ix = Instruction::new_with_borsh(
        program_id,
        &GeolocationInstruction::AddParentDevice,
        vec![
            AccountMeta::new(probe_pda, false),
            AccountMeta::new_readonly(device, false),
            AccountMeta::new_readonly(program_config_pda, false),
            AccountMeta::new_readonly(globalstate, false),
            AccountMeta::new(payer.pubkey(), true),
            AccountMeta::new_readonly(solana_sdk_ids::system_program::id(), false),
        ],
    );
    let tx = Transaction::new_signed_with_payer(
        &[ix],
        Some(&payer.pubkey()),
        &[&payer],
        context.last_blockhash,
    );
    context.banks_client.process_transaction(tx).await.unwrap();

    let probe_account = context
        .banks_client
        .get_account(probe_pda)
        .await
        .unwrap()
        .unwrap();
    let probe = GeoProbe::try_from(&probe_account.data[..]).unwrap();
    assert_eq!(probe.parent_devices, vec![device]);
}

#[tokio::test]
async fn test_add_parent_device_max_size_device_short_names() {
    add_max_size_parent_device(|i| format!("Vlan{i}")).await;
}

#[tokio::test]
async fn test_add_parent_device_max_size_device_long_names() {
    add_max_size_parent_device(|i| format!("Ethernet{i}/{}1", "0".repeat(490))).await;
}
