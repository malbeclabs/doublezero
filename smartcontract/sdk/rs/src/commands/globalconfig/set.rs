use crate::{DoubleZeroClient, GetGlobalConfigCommand, GetGlobalStateCommand};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalconfig_pda, get_resource_extension_pda},
    processors::globalconfig::set::SetGlobalConfigArgs,
    resource::ResourceType,
    state::globalconfig::GlobalConfig,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetGlobalConfigCommand {
    pub local_asn: Option<u32>,
    pub remote_asn: Option<u32>,
    pub device_tunnel_block: Option<NetworkV4>,
    pub user_tunnel_block: Option<NetworkV4>,
    pub multicastgroup_block: Option<NetworkV4>,
    pub next_bgp_community: Option<u16>,
}

impl SetGlobalConfigCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let global_config = GetGlobalConfigCommand.execute(client).ok();
        let set_config_args = self.merge_config_updates(global_config)?;

        let (pda_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
        let (device_tunnel_block_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::DeviceTunnelBlock);
        let (user_tunnel_block_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::UserTunnelBlock);
        let (multicastgroup_block_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::MulticastGroupBlock);
        let (link_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::LinkIds);
        let (segment_routing_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::SegmentRoutingIds);

        client.execute_transaction(
            DoubleZeroInstruction::SetGlobalConfig(set_config_args),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(device_tunnel_block_pda, false),
                AccountMeta::new(user_tunnel_block_pda, false),
                AccountMeta::new(multicastgroup_block_pda, false),
                AccountMeta::new(link_ids_pda, false),
                AccountMeta::new(segment_routing_ids_pda, false),
            ],
        )
    }

    fn merge_config_updates(
        &self,
        global_config: Option<(Pubkey, GlobalConfig)>,
    ) -> eyre::Result<SetGlobalConfigArgs> {
        match (self, global_config) {
            (
                SetGlobalConfigCommand {
                    local_asn: None,
                    remote_asn: None,
                    device_tunnel_block: None,
                    user_tunnel_block: None,
                    multicastgroup_block: None,
                    next_bgp_community: None,
                },
                _,
            ) => Err(eyre::eyre!(
                "Invalid SetGlobalConfigCommand; no updates specified"
            )),
            (
                SetGlobalConfigCommand {
                    local_asn: Some(local_asn),
                    remote_asn: Some(remote_asn),
                    device_tunnel_block: Some(device_tunnel_block),
                    user_tunnel_block: Some(user_tunnel_block),
                    multicastgroup_block: Some(multicastgroup_block),
                    next_bgp_community,
                },
                _,
            ) => Ok(SetGlobalConfigArgs {
                local_asn: *local_asn,
                remote_asn: *remote_asn,
                device_tunnel_block: *device_tunnel_block,
                user_tunnel_block: *user_tunnel_block,
                multicastgroup_block: *multicastgroup_block,
                next_bgp_community: *next_bgp_community,
            }),
            (_, None) => Err(eyre::eyre!("Invalid SetGlobalConfigCommand; incomplete set command with no valid config to update")),
            (set_config_command, Some((_, existing_config))) => Ok(SetGlobalConfigArgs {
                local_asn: set_config_command.local_asn.unwrap_or(existing_config.local_asn),
                remote_asn: set_config_command.remote_asn.unwrap_or(existing_config.remote_asn),
                device_tunnel_block: set_config_command.device_tunnel_block.unwrap_or(existing_config.device_tunnel_block),
                user_tunnel_block: set_config_command.user_tunnel_block.unwrap_or(existing_config.user_tunnel_block),
                multicastgroup_block: set_config_command.multicastgroup_block.unwrap_or(existing_config.multicastgroup_block),
                next_bgp_community: set_config_command.next_bgp_community,
            }),
        }
    }
}
