use crate::{
    error::DoubleZeroError,
    pda::{get_device_pda, get_globalstate_pda},
    processors::validation::validate_program_account,
    serializer::try_acc_write,
    state::{
        accounttype::AccountType, device::Device, globalstate::GlobalState, interface::Interface,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct MigrateDeviceInterfacesArgs {}

impl fmt::Debug for MigrateDeviceInterfacesArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "MigrateDeviceInterfacesArgs")
    }
}

/// Deserializes a Device account using the legacy interface format (pre-RFC-18),
/// where Interface discriminant 1 does NOT have trailing flex_algo_node_segments bytes.
/// Also handles the current V3 format (discriminant 3) for idempotency detection.
fn deserialize_device_legacy(data: &[u8]) -> Result<Device, ProgramError> {
    let mut reader = data;

    let account_type: AccountType =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let owner: solana_program::pubkey::Pubkey =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let index: u128 = borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let bump_seed: u8 = borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let location_pk: solana_program::pubkey::Pubkey =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let exchange_pk: solana_program::pubkey::Pubkey =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let device_type: crate::state::device::DeviceType =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let public_ip: std::net::Ipv4Addr =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or([0, 0, 0, 0].into());
    let status: crate::state::device::DeviceStatus =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let code: String = borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let dz_prefixes: doublezero_program_common::types::NetworkV4List =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let metrics_publisher_pk: solana_program::pubkey::Pubkey =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let contributor_pk: solana_program::pubkey::Pubkey =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let mgmt_vrf: String = borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();

    // Read the interfaces vec using the legacy format.
    // Borsh encodes Vec<T> as: [u32 length][T...] — we read the count manually,
    // then deserialize each interface using the legacy reader.
    let iface_count: u32 = borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let mut interfaces: Vec<Interface> = Vec::with_capacity(iface_count as usize);
    for _ in 0..iface_count {
        // Read the discriminant byte to determine interface version.
        let discriminant: u8 =
            borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
        let iface = match discriminant {
            0 => {
                let v1: crate::state::interface::InterfaceV1 =
                    borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
                Interface::V1(v1)
            }
            1 | 2 => {
                // V2 format — no flex_algo_node_segments bytes on disk.
                let v2: crate::state::interface::InterfaceV2 =
                    borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
                Interface::V2(v2)
            }
            3 => {
                // V3 format — includes flex_algo_node_segments.
                let v3: crate::state::interface::InterfaceV3 =
                    borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
                Interface::V3(v3)
            }
            _ => Interface::V3(crate::state::interface::InterfaceV3::default()),
        };
        interfaces.push(iface);
    }

    let reference_count: u32 =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let users_count: u16 = borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let max_users: u16 = borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let device_health: crate::state::device::DeviceHealth =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let desired_status: crate::state::device::DeviceDesiredStatus =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let unicast_users_count: u16 =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let multicast_subscribers_count: u16 =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let max_unicast_users: u16 =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let max_multicast_subscribers: u16 =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let reserved_seats: u16 = borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let multicast_publishers_count: u16 =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();
    let max_multicast_publishers: u16 =
        borsh::BorshDeserialize::deserialize(&mut reader).unwrap_or_default();

    if account_type != AccountType::Device {
        return Err(ProgramError::InvalidAccountData);
    }

    Ok(Device {
        account_type,
        owner,
        index,
        bump_seed,
        location_pk,
        exchange_pk,
        device_type,
        public_ip,
        status,
        code,
        dz_prefixes,
        metrics_publisher_pk,
        contributor_pk,
        mgmt_vrf,
        interfaces,
        reference_count,
        users_count,
        max_users,
        device_health,
        desired_status,
        unicast_users_count,
        multicast_subscribers_count,
        max_unicast_users,
        max_multicast_subscribers,
        reserved_seats,
        multicast_publishers_count,
        max_multicast_publishers,
    })
}

/// Migrates a Device account's interfaces from the pre-RFC-18 on-chain format
/// (Interface discriminant 1, no flex_algo_node_segments bytes) to the current
/// V3 format (Interface discriminant 3, with an empty flex_algo_node_segments vec).
///
/// This instruction is idempotent: calling it on an already-migrated account is
/// a no-op. This is safe for the activator startup sweep, which calls it for all
/// devices without knowing which ones are already in the new format.
///
/// Accounts expected:
///   0. `device_account`    — writable, owned by this program
///   1. `globalstate_account` — readable, used for authorization
///   2. `payer_account`     — signer (foundation, device owner, or activator authority)
///   3. `system_program`    — for account resizing
pub fn process_migrate_device_interfaces(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &MigrateDeviceInterfacesArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_migrate_device_interfaces");

    if !payer_account.is_signer {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Owner + non-empty + writable checks before we trust the on-disk bytes.
    validate_program_account!(device_account, program_id, writable = true, "Device");
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        pda = &get_globalstate_pda(program_id).0,
        "GlobalState"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;

    // Read the device using the legacy deserializer. The borrow and the data_slice
    // derived from it are scoped to this block so the immutable borrow on
    // device_account.data is released before try_acc_write takes a mutable borrow.
    let (mut device, already_migrated) = {
        let data_borrow = device_account.data.borrow();
        let device = deserialize_device_legacy(&data_borrow)?;
        // If any interface is already V3, the account has been migrated.
        let already_migrated = device
            .interfaces
            .iter()
            .any(|i| matches!(i, Interface::V3(_)));
        (device, already_migrated)
    };

    // Now that we have the device index, verify the PDA.
    assert_eq!(
        device_account.key,
        &get_device_pda(program_id, device.index).0,
        "Invalid Device PDA"
    );

    // Authorization: payer must be the foundation, the device owner, or the activator
    // authority. The activator calls this during its startup sweep.
    let is_foundation = globalstate.foundation_allowlist.contains(payer_account.key);
    let is_owner = device.owner == *payer_account.key;
    let is_activator = globalstate.activator_authority_pk == *payer_account.key;
    if !is_foundation && !is_owner && !is_activator {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Idempotency check: the account already has V3 interfaces — skip migration
    // so we don't zero any topology assignments in the flex_algo_node_segments vecs.
    if already_migrated {
        return Ok(());
    }

    // Convert all V1/V2 interfaces to V3 (adding empty flex_algo_node_segments vec).
    let migrated: Vec<Interface> = device
        .interfaces
        .iter()
        .map(|iface| Interface::V3(iface.into_v3()))
        .collect();
    device.interfaces = migrated;

    // Write back with the V3 format — each interface now includes the
    // (empty) flex_algo_node_segments vec in its serialized form.
    try_acc_write(&device, device_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Migrated device: {}", device.code);

    Ok(())
}
