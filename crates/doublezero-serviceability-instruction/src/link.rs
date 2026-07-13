//! Link-domain instruction builders.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_link_pda, get_resource_extension_pda, get_topology_pda},
    processors::link::create::LinkCreateArgs,
    resource::ResourceType,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// The unicast-default topology account is required; `CreateLink` auto-tags the
/// link into it.
const UNICAST_DEFAULT_TOPOLOGY: &str = "unicast-default";

/// `CreateLink` (variant 28).
///
/// Account layout (processor `next_account_info` order), before the trailing
/// `[payer, system]` appended by [`common::build`]:
///
/// ```text
/// link                       (writable)  — PDA get_link_pda(link_index)
/// contributor                (writable)
/// side_a                     (writable)
/// side_z                     (writable)
/// globalstate                (writable)
/// unicast_default_topology   (writable)  — get_topology_pda("unicast-default")
/// device_tunnel_block        (writable)  — ResourceType::DeviceTunnelBlock
/// link_ids                   (writable)  — ResourceType::LinkIds
/// ```
pub fn create_link(
    program_id: &Pubkey,
    payer: &Pubkey,
    contributor: &Pubkey,
    side_a: &Pubkey,
    side_z: &Pubkey,
    link_index: u128,
    args: LinkCreateArgs,
) -> Instruction {
    let (link, _) = get_link_pda(program_id, link_index);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (device_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
    let (link_ids, _, _) = get_resource_extension_pda(program_id, ResourceType::LinkIds);
    let (unicast_default_topology, _) = get_topology_pda(program_id, UNICAST_DEFAULT_TOPOLOGY);

    let accounts = vec![
        AccountMeta::new(link, false),
        AccountMeta::new(*contributor, false),
        AccountMeta::new(*side_a, false),
        AccountMeta::new(*side_z, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(unicast_default_topology, false),
        AccountMeta::new(device_tunnel_block, false),
        AccountMeta::new(link_ids, false),
    ];

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateLink(args),
        accounts,
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_serviceability::state::link::LinkLinkType;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_create_link_accounts_and_tag() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let contributor = Pubkey::new_unique();
        let side_a = Pubkey::new_unique();
        let side_z = Pubkey::new_unique();

        let args = LinkCreateArgs {
            code: "link1".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1_000_000,
            jitter_ns: 100_000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: None,
            use_onchain_allocation: true,
        };

        let ix = create_link(&pid, &payer, &contributor, &side_a, &side_z, 1, args);

        assert_eq!(ix.data[0], 28);
        assert_eq!(ix.program_id, pid);

        let (link, _) = get_link_pda(&pid, 1);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (device_tunnel_block, _, _) =
            get_resource_extension_pda(&pid, ResourceType::DeviceTunnelBlock);
        let (link_ids, _, _) = get_resource_extension_pda(&pid, ResourceType::LinkIds);
        let (unicast_default, _) = get_topology_pda(&pid, UNICAST_DEFAULT_TOPOLOGY);

        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(link, false),
                AccountMeta::new(contributor, false),
                AccountMeta::new(side_a, false),
                AccountMeta::new(side_z, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(unicast_default, false),
                AccountMeta::new(device_tunnel_block, false),
                AccountMeta::new(link_ids, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }
}
