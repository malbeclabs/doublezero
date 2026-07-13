//! Resource-extension-domain instruction builders.
//!
//! All route through `authorize()` -> [`common::build_with_permission`]. The
//! resource-extension PDA and its associated account are derived from
//! `args.resource_type` (a data-bearing enum).

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalconfig_pda, get_globalstate_pda, get_resource_extension_pda},
    processors::resource::{
        allocate::ResourceAllocateArgs, closeaccount::ResourceExtensionCloseAccountArgs,
        create::ResourceCreateArgs, deallocate::ResourceDeallocateArgs,
    },
    resource::ResourceType,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// The account a resource extension is associated with: the entity pubkey for
/// per-entity resources (`DzPrefixBlock`, `TunnelIds`), else the system program.
fn associated_account(resource_type: ResourceType) -> Pubkey {
    match resource_type {
        ResourceType::DzPrefixBlock(pk, _) | ResourceType::TunnelIds(pk, _) => pk,
        _ => Pubkey::default(),
    }
}

/// `AllocateResource` (variant 80).
/// Accounts: `[resource, associated_account, globalstate]`.
pub fn allocate_resource(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: ResourceAllocateArgs,
) -> Instruction {
    let (resource, _, _) = get_resource_extension_pda(program_id, args.resource_type);
    let associated = associated_account(args.resource_type);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::AllocateResource(args),
        vec![
            AccountMeta::new(resource, false),
            AccountMeta::new(associated, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `CreateResource` (variant 81).
/// Accounts: `[resource, associated_account, globalstate, globalconfig]`.
pub fn create_resource(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: ResourceCreateArgs,
) -> Instruction {
    let (resource, _, _) = get_resource_extension_pda(program_id, args.resource_type);
    let associated = associated_account(args.resource_type);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (globalconfig, _) = get_globalconfig_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateResource(args),
        vec![
            AccountMeta::new(resource, false),
            AccountMeta::new(associated, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(globalconfig, false),
        ],
        payer,
    )
}

/// `DeallocateResource` (variant 82).
/// Accounts: `[resource, associated_account, globalstate]`.
pub fn deallocate_resource(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: ResourceDeallocateArgs,
) -> Instruction {
    let (resource, _, _) = get_resource_extension_pda(program_id, args.resource_type);
    let associated = associated_account(args.resource_type);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeallocateResource(args),
        vec![
            AccountMeta::new(resource, false),
            AccountMeta::new(associated, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `CloseResource` (variant 85). Accounts: `[resource, owner, globalstate]`.
///
/// Takes the resource pubkey and its owner directly (both onchain-read on the
/// close path).
pub fn close_resource(
    program_id: &Pubkey,
    payer: &Pubkey,
    resource: &Pubkey,
    owner: &Pubkey,
    args: ResourceExtensionCloseAccountArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CloseResource(args),
        vec![
            AccountMeta::new(*resource, false),
            AccountMeta::new(*owner, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_serviceability::resource::IdOrIp;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_allocate_resource_no_associated_for_global_pool() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        // A global pool (DeviceTunnelBlock) -> associated account is the default pubkey.
        let args = ResourceAllocateArgs {
            resource_type: ResourceType::DeviceTunnelBlock,
            requested: None,
        };
        let ix = allocate_resource(&pid, &payer, args);
        assert_eq!(ix.data[0], 80);
        let (resource, _, _) = get_resource_extension_pda(&pid, ResourceType::DeviceTunnelBlock);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(resource, false),
                AccountMeta::new(Pubkey::default(), false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_create_resource_per_entity_uses_entity_as_associated() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let args = ResourceCreateArgs {
            resource_type: ResourceType::TunnelIds(device, 0),
        };
        let ix = create_resource(&pid, &payer, args);
        assert_eq!(ix.data[0], 81);
        let (resource, _, _) = get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (globalconfig, _) = get_globalconfig_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(resource, false),
                AccountMeta::new(device, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(globalconfig, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_deallocate_resource() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let args = ResourceDeallocateArgs {
            resource_type: ResourceType::LinkIds,
            value: IdOrIp::Id(1),
        };
        let ix = deallocate_resource(&pid, &payer, args);
        assert_eq!(ix.data[0], 82);
        assert_eq!(ix.accounts.len(), 3 + 2);
    }

    #[test]
    fn test_close_resource() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let resource = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let ix = close_resource(
            &pid,
            &payer,
            &resource,
            &owner,
            ResourceExtensionCloseAccountArgs {},
        );
        assert_eq!(ix.data[0], 85);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(resource, false),
                AccountMeta::new(owner, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }
}
