//! Tenant-domain instruction builders.
//!
//! All route through `authorize()` -> [`common::build_with_permission`]. Note the
//! globalstate account is passed writable on `create` and read-only on every
//! other verb — this mirrors the SDK command byte-for-byte (the golden-fixture
//! parity target). The create processor itself only *reads* globalstate; the new
//! tenant's id is allocated from the `VrfIds` resource-extension account, not from
//! `globalstate.account_index`.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_resource_extension_pda, get_tenant_pda},
    processors::tenant::{
        add_administrator::TenantAddAdministratorArgs, create::TenantCreateArgs,
        delete::TenantDeleteArgs, remove_administrator::TenantRemoveAdministratorArgs,
        update::TenantUpdateArgs, update_payment_status::UpdatePaymentStatusArgs,
    },
    resource::ResourceType,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateTenant` (variant 88). Accounts: `[tenant, globalstate(w), vrf_ids(w)]`.
///
/// The tenant PDA is derived from `args.code`.
pub fn create_tenant(program_id: &Pubkey, payer: &Pubkey, args: TenantCreateArgs) -> Instruction {
    let (tenant, _) = get_tenant_pda(program_id, &args.code);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (vrf_ids, _, _) = get_resource_extension_pda(program_id, ResourceType::VrfIds);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateTenant(args),
        vec![
            AccountMeta::new(tenant, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(vrf_ids, false),
        ],
        payer,
    )
}

/// `UpdateTenant` (variant 89). Accounts: `[tenant, globalstate(readonly)]`.
pub fn update_tenant(
    program_id: &Pubkey,
    payer: &Pubkey,
    tenant: &Pubkey,
    args: TenantUpdateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateTenant(args),
        vec![
            AccountMeta::new(*tenant, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `DeleteTenant` (variant 90). Accounts: `[tenant, globalstate(readonly), vrf_ids(w)]`.
pub fn delete_tenant(
    program_id: &Pubkey,
    payer: &Pubkey,
    tenant: &Pubkey,
    args: TenantDeleteArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (vrf_ids, _, _) = get_resource_extension_pda(program_id, ResourceType::VrfIds);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteTenant(args),
        vec![
            AccountMeta::new(*tenant, false),
            AccountMeta::new_readonly(globalstate, false),
            AccountMeta::new(vrf_ids, false),
        ],
        payer,
    )
}

/// `TenantAddAdministrator` (variant 91). Accounts: `[tenant, globalstate(readonly)]`.
pub fn tenant_add_administrator(
    program_id: &Pubkey,
    payer: &Pubkey,
    tenant: &Pubkey,
    args: TenantAddAdministratorArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::TenantAddAdministrator(args),
        vec![
            AccountMeta::new(*tenant, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `TenantRemoveAdministrator` (variant 92). Accounts: `[tenant, globalstate(readonly)]`.
pub fn tenant_remove_administrator(
    program_id: &Pubkey,
    payer: &Pubkey,
    tenant: &Pubkey,
    args: TenantRemoveAdministratorArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::TenantRemoveAdministrator(args),
        vec![
            AccountMeta::new(*tenant, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `UpdatePaymentStatus` (variant 93). Accounts: `[tenant, globalstate(readonly)]`.
pub fn update_payment_status(
    program_id: &Pubkey,
    payer: &Pubkey,
    tenant: &Pubkey,
    args: UpdatePaymentStatusArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdatePaymentStatus(args),
        vec![
            AccountMeta::new(*tenant, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_create_tenant() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let args = TenantCreateArgs {
            code: "acme".to_string(),
            ..Default::default()
        };
        let ix = create_tenant(&pid, &payer, args);
        assert_eq!(ix.data[0], 88);
        let (tenant, _) = get_tenant_pda(&pid, "acme");
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (vrf_ids, _, _) = get_resource_extension_pda(&pid, ResourceType::VrfIds);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(tenant, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(vrf_ids, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_delete_tenant() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let tenant = Pubkey::new_unique();
        let ix = delete_tenant(&pid, &payer, &tenant, TenantDeleteArgs {});
        assert_eq!(ix.data[0], 90);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (vrf_ids, _, _) = get_resource_extension_pda(&pid, ResourceType::VrfIds);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(tenant, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(vrf_ids, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_tenant_readonly_globalstate_verbs() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let tenant = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let expected = vec![
            AccountMeta::new(tenant, false),
            AccountMeta::new_readonly(globalstate, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
        ];
        for (ix, tag) in [
            (
                update_tenant(&pid, &payer, &tenant, TenantUpdateArgs::default()),
                89,
            ),
            (
                tenant_add_administrator(
                    &pid,
                    &payer,
                    &tenant,
                    TenantAddAdministratorArgs::default(),
                ),
                91,
            ),
            (
                tenant_remove_administrator(
                    &pid,
                    &payer,
                    &tenant,
                    TenantRemoveAdministratorArgs::default(),
                ),
                92,
            ),
            (
                update_payment_status(&pid, &payer, &tenant, UpdatePaymentStatusArgs::default()),
                93,
            ),
        ] {
            assert_eq!(ix.data[0], tag);
            assert_eq!(ix.accounts, expected);
        }
    }
}
