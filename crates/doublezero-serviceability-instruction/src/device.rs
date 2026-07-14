//! Device-domain instruction builders.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_device_pda, get_globalconfig_pda, get_globalstate_pda, get_resource_extension_pda},
    processors::device::{create::DeviceCreateArgs, delete::DeviceDeleteArgs},
    resource::ResourceType,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateDevice` (variant 20).
///
/// Account layout (processor `next_account_info` order), before the trailing
/// `[payer, system]` appended by `common::build_with_permission`:
///
/// ```text
/// device                (writable)  — PDA get_device_pda(device_index)
/// contributor           (writable)
/// location              (writable)
/// exchange              (writable)
/// globalstate           (writable)
/// globalconfig          (writable)
/// tunnel_ids resource   (writable)  — ResourceType::TunnelIds(device, 0)
/// dz_prefix_block[i]    (writable)  — one per args.dz_prefixes entry
/// ```
///
/// The `dz_prefix` blocks and `args.resource_count` are produced from the same
/// loop, so the declared count can never disagree with the account list.
///
/// `device_index` is the **new** device's index: the caller passes
/// `globalstate.account_index + 1`, not the raw current value.
pub fn create_device(
    program_id: &Pubkey,
    payer: &Pubkey,
    contributor: &Pubkey,
    location: &Pubkey,
    exchange: &Pubkey,
    device_index: u128,
    mut args: DeviceCreateArgs,
) -> Instruction {
    let (device, _) = get_device_pda(program_id, device_index);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (globalconfig, _) = get_globalconfig_pda(program_id);
    let (tunnel_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::TunnelIds(device, 0));

    let mut accounts = vec![
        AccountMeta::new(device, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(*location, false),
        AccountMeta::new(*exchange, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(globalconfig, false),
        AccountMeta::new(tunnel_ids, false),
    ];

    let dz_prefix_count = args.dz_prefixes.len();
    for idx in 0..dz_prefix_count {
        let (dz_prefix, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DzPrefixBlock(device, idx));
        accounts.push(AccountMeta::new(dz_prefix, false));
    }

    // One TunnelIds account plus one DzPrefixBlock per advertised prefix, derived
    // from the same loop that produced the accounts above. The count is bounded
    // by the transaction's account budget, so overflow is unreachable in practice;
    // panicking is strictly better than emitting a `resource_count` that disagrees
    // with the account list — the exact invariant this crate exists to protect.
    let resource_total = 1 + dz_prefix_count;
    args.resource_count =
        u8::try_from(resource_total).expect("device resource_count exceeds u8::MAX");

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateDevice(args),
        accounts,
        payer,
    )
}

/// How `delete_device` closes the device's resource accounts. Selecting the
/// path explicitly (rather than inferring it from an empty slice) prevents an
/// activated device from silently getting the legacy layout.
pub enum DeviceDeleteResources<'a> {
    /// Never-activated device with no live resource accounts.
    Legacy,
    /// Activated device: atomically close its resource accounts. `owners[i]` is
    /// the onchain-read owner of resource `i` (idx 0 = `TunnelIds(device, 0)`,
    /// idx 1.. = `DzPrefixBlock(device, idx - 1)`), not offline-derivable.
    Atomic {
        location: &'a Pubkey,
        exchange: &'a Pubkey,
        owners: &'a [Pubkey],
        device_owner: &'a Pubkey,
    },
}

/// `DeleteDevice` (variant 26).
///
/// Two layouts, selected by [`DeviceDeleteResources`]:
///
/// - [`DeviceDeleteResources::Legacy`]:
///   ```text
///   device       (writable)
///   contributor  (writable)
///   globalstate  (writable)
///   ```
/// - [`DeviceDeleteResources::Atomic`]:
///   ```text
///   device                (writable)
///   contributor           (writable)
///   globalstate           (writable)
///   location              (writable)
///   exchange              (writable)
///   resource[i]           (writable)  — idx 0: TunnelIds(device, 0);
///                                        idx 1..: DzPrefixBlock(device, idx-1)
///   resource_owner[i]     (writable)  — the onchain owner of resource[i]
///   device_owner          (writable)
///   ```
///
/// `owners.len()` drives both the resource-PDA loop and `args.resource_count`.
///
/// `process_delete_device` routes through `authorize()` (NETWORK_ADMIN, for the
/// non-contributor override), so this builder is assigned to
/// `common::build_with_permission` and will carry a trailing Permission PDA once
/// the permission model is activated (deferred today, like every other assigned
/// builder).
pub fn delete_device(
    program_id: &Pubkey,
    payer: &Pubkey,
    device: &Pubkey,
    contributor: &Pubkey,
    resources: DeviceDeleteResources,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);

    let mut accounts = vec![
        AccountMeta::new(*device, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(globalstate, false),
    ];

    let (location, exchange, owners, device_owner) = match resources {
        DeviceDeleteResources::Legacy => {
            return common::build_with_permission(
                program_id,
                DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs::default()),
                accounts,
                payer,
            );
        }
        DeviceDeleteResources::Atomic {
            location,
            exchange,
            owners,
            device_owner,
        } => (location, exchange, owners, device_owner),
    };

    accounts.push(AccountMeta::new(*location, false));
    accounts.push(AccountMeta::new(*exchange, false));
    // Resource PDAs, in the order the processor consumes them.
    for idx in 0..owners.len() {
        let resource_type = if idx == 0 {
            ResourceType::TunnelIds(*device, 0)
        } else {
            ResourceType::DzPrefixBlock(*device, idx - 1)
        };
        let (pda, _, _) = get_resource_extension_pda(program_id, resource_type);
        accounts.push(AccountMeta::new(pda, false));
    }
    // Then the owner of each resource account, in the same order.
    for owner in owners {
        accounts.push(AccountMeta::new(*owner, false));
    }
    accounts.push(AccountMeta::new(*device_owner, false));

    // Bounded by the transaction's account budget, so overflow is unreachable;
    // panicking is strictly better than emitting a `resource_count` that disagrees
    // with the resource account list.
    let resource_count =
        u8::try_from(owners.len()).expect("device delete resource_count exceeds u8::MAX");
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs { resource_count }),
        accounts,
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_serviceability::state::device::DeviceType;
    use solana_system_interface::program as system_program;

    fn program_id() -> Pubkey {
        Pubkey::new_unique()
    }

    #[test]
    fn test_create_device_accounts_and_tag() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let location = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();

        let args = DeviceCreateArgs {
            code: "dev1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.0/8".parse().unwrap(),
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
            resource_count: 0,
        };

        let ix = create_device(&pid, &payer, &contributor, &location, &exchange, 1, args);

        // Tag byte for CreateDevice.
        assert_eq!(ix.data[0], 20);
        assert_eq!(ix.program_id, pid);

        let (device, _) = get_device_pda(&pid, 1);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (globalconfig, _) = get_globalconfig_pda(&pid);
        let (tunnel_ids, _, _) =
            get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz_prefix0, _, _) =
            get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));

        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(location, false),
                AccountMeta::new(exchange, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(globalconfig, false),
                AccountMeta::new(tunnel_ids, false),
                AccountMeta::new(dz_prefix0, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_create_device_writes_resource_count() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        // Two prefixes -> resource_count = 1 (TunnelIds) + 2 = 3.
        let args = DeviceCreateArgs {
            code: "dev1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.0/8,11.0.0.0/8".parse().unwrap(),
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: None,
            resource_count: 0,
        };
        let ix = create_device(
            &pid,
            &payer,
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            1,
            args,
        );
        let decoded = DoubleZeroInstruction::unpack(&ix.data).unwrap();
        match decoded {
            DoubleZeroInstruction::CreateDevice(a) => assert_eq!(a.resource_count, 3),
            other => panic!("unexpected variant: {other:?}"),
        }
        // account list: 7 fixed + 2 dz_prefix + payer + system = 11
        assert_eq!(ix.accounts.len(), 11);
    }

    #[test]
    fn test_delete_device_legacy() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();

        let ix = delete_device(
            &pid,
            &payer,
            &device,
            &contributor,
            DeviceDeleteResources::Legacy,
        );

        assert_eq!(ix.data[0], 26);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_delete_device_atomic() {
        let pid = program_id();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let location = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();
        let res_owner = Pubkey::new_unique();
        let device_owner = Pubkey::new_unique();

        // One TunnelIds + one DzPrefixBlock -> two resource owners.
        let ix = delete_device(
            &pid,
            &payer,
            &device,
            &contributor,
            DeviceDeleteResources::Atomic {
                location: &location,
                exchange: &exchange,
                owners: &[res_owner, res_owner],
                device_owner: &device_owner,
            },
        );

        let decoded = DoubleZeroInstruction::unpack(&ix.data).unwrap();
        match decoded {
            DoubleZeroInstruction::DeleteDevice(a) => assert_eq!(a.resource_count, 2),
            other => panic!("unexpected variant: {other:?}"),
        }

        let (globalstate, _) = get_globalstate_pda(&pid);
        let (tunnel_ids, _, _) =
            get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz_prefix0, _, _) =
            get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(device, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(location, false),
                AccountMeta::new(exchange, false),
                AccountMeta::new(tunnel_ids, false),
                AccountMeta::new(dz_prefix0, false),
                AccountMeta::new(res_owner, false),
                AccountMeta::new(res_owner, false),
                AccountMeta::new(device_owner, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }
}
