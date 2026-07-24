//! GlobalConfig-domain instruction builder.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalconfig_pda, get_globalstate_pda, get_resource_extension_pda},
    processors::globalconfig::set::SetGlobalConfigArgs,
    resource::ResourceType,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `SetGlobalConfig` (variant 3).
///
/// Accounts: `[globalconfig, globalstate, device_tunnel_block, user_tunnel_block,
/// multicast_group_block, link_ids, segment_routing_ids, multicast_publisher_block,
/// vrf_ids, admin_group_bits]` — the config PDA plus every resource-extension pool
/// (so the program can re-seed pools when blocks change). Routes through
/// `authorize()` -> [`common::build_with_permission`].
pub fn set_global_config(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: SetGlobalConfigArgs,
) -> Instruction {
    let (globalconfig, _) = get_globalconfig_pda(program_id);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (device_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
    let (multicast_group_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
    let (link_ids, _, _) = get_resource_extension_pda(program_id, ResourceType::LinkIds);
    let (segment_routing_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
    let (vrf_ids, _, _) = get_resource_extension_pda(program_id, ResourceType::VrfIds);
    let (admin_group_bits, _, _) =
        get_resource_extension_pda(program_id, ResourceType::AdminGroupBits);

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(args),
        vec![
            AccountMeta::new(globalconfig, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(device_tunnel_block, false),
            AccountMeta::new(user_tunnel_block, false),
            AccountMeta::new(multicast_group_block, false),
            AccountMeta::new(link_ids, false),
            AccountMeta::new(segment_routing_ids, false),
            AccountMeta::new(multicast_publisher_block, false),
            AccountMeta::new(vrf_ids, false),
            AccountMeta::new(admin_group_bits, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_set_global_config_account_order() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let ix = set_global_config(&pid, &payer, SetGlobalConfigArgs::default());
        assert_eq!(ix.data[0], 3);
        let (globalconfig, _) = get_globalconfig_pda(&pid);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let ext = |rt| get_resource_extension_pda(&pid, rt).0;
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(globalconfig, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(ext(ResourceType::DeviceTunnelBlock), false),
                AccountMeta::new(ext(ResourceType::UserTunnelBlock), false),
                AccountMeta::new(ext(ResourceType::MulticastGroupBlock), false),
                AccountMeta::new(ext(ResourceType::LinkIds), false),
                AccountMeta::new(ext(ResourceType::SegmentRoutingIds), false),
                AccountMeta::new(ext(ResourceType::MulticastPublisherBlock), false),
                AccountMeta::new(ext(ResourceType::VrfIds), false),
                AccountMeta::new(ext(ResourceType::AdminGroupBits), false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }
}
