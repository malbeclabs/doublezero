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

pub const SET_GLOBAL_CONFIG_COMPUTE_UNIT_LIMIT: u32 = 1_400_000;

#[derive(Debug, PartialEq, Clone)]
pub struct SetGlobalConfigCommand {
    pub local_asn: Option<u32>,
    pub remote_asn: Option<u32>,
    pub device_tunnel_block: Option<NetworkV4>,
    pub user_tunnel_block: Option<NetworkV4>,
    pub multicastgroup_block: Option<NetworkV4>,
    pub next_bgp_community: Option<u16>,
    pub multicast_publisher_block: Option<NetworkV4>,
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
        let (multicast_publisher_block_pda, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            ResourceType::MulticastPublisherBlock,
        );
        let (vrf_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::VrfIds);
        let (admin_group_bits_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::AdminGroupBits);

        client.execute_transaction_with_compute_unit_limit(
            DoubleZeroInstruction::SetGlobalConfig(set_config_args),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
                AccountMeta::new(device_tunnel_block_pda, false),
                AccountMeta::new(user_tunnel_block_pda, false),
                AccountMeta::new(multicastgroup_block_pda, false),
                AccountMeta::new(link_ids_pda, false),
                AccountMeta::new(segment_routing_ids_pda, false),
                AccountMeta::new(multicast_publisher_block_pda, false),
                AccountMeta::new(vrf_ids_pda, false),
                AccountMeta::new(admin_group_bits_pda, false),
            ],
            SET_GLOBAL_CONFIG_COMPUTE_UNIT_LIMIT,
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
                    multicast_publisher_block: None,
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
                    multicast_publisher_block: Some(multicast_publisher_block),
                },
                _,
            ) => Ok(SetGlobalConfigArgs {
                local_asn: *local_asn,
                remote_asn: *remote_asn,
                device_tunnel_block: *device_tunnel_block,
                user_tunnel_block: *user_tunnel_block,
                multicastgroup_block: *multicastgroup_block,
                next_bgp_community: *next_bgp_community,
                multicast_publisher_block: *multicast_publisher_block,
            }),
            (_, None) => Err(eyre::eyre!("Invalid SetGlobalConfigCommand; incomplete set command with no valid config to update")),
            (set_config_command, Some((_, existing_config))) => Ok(SetGlobalConfigArgs {
                local_asn: set_config_command.local_asn.unwrap_or(existing_config.local_asn),
                remote_asn: set_config_command.remote_asn.unwrap_or(existing_config.remote_asn),
                device_tunnel_block: set_config_command.device_tunnel_block.unwrap_or(existing_config.device_tunnel_block),
                user_tunnel_block: set_config_command.user_tunnel_block.unwrap_or(existing_config.user_tunnel_block),
                multicastgroup_block: set_config_command.multicastgroup_block.unwrap_or(existing_config.multicastgroup_block),
                next_bgp_community: set_config_command.next_bgp_community,
                multicast_publisher_block: set_config_command
                    .multicast_publisher_block
                    .unwrap_or(existing_config.multicast_publisher_block),
            }),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{SetGlobalConfigCommand, SET_GLOBAL_CONFIG_COMPUTE_UNIT_LIMIT};
    use crate::{tests::utils::create_test_client, DoubleZeroClient};
    use doublezero_serviceability::pda::get_globalconfig_pda;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_setglobalconfig_uses_compute_unit_limit() {
        let mut client = create_test_client();

        // GetGlobalConfigCommand fetches the globalconfig PDA; return an error so
        // merge_config_updates takes the "all fields Some" branch rather than merging.
        let (globalconfig_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
        client
            .expect_get()
            .with(predicate::eq(globalconfig_pubkey))
            .returning(|_| Err(eyre::eyre!("not initialized")));

        client
            .expect_execute_transaction_with_compute_unit_limit()
            .withf(|_, _, limit| *limit == SET_GLOBAL_CONFIG_COMPUTE_UNIT_LIMIT)
            .times(1)
            .returning(|_, _, _| Ok(Signature::new_unique()));

        let res = SetGlobalConfigCommand {
            local_asn: Some(65000),
            remote_asn: Some(65001),
            device_tunnel_block: Some("10.0.0.0/16".parse().unwrap()),
            user_tunnel_block: Some("10.1.0.0/16".parse().unwrap()),
            multicastgroup_block: Some("239.0.0.0/16".parse().unwrap()),
            next_bgp_community: Some(100),
            multicast_publisher_block: Some("239.1.0.0/16".parse().unwrap()),
        }
        .execute(&client);
        assert!(res.is_ok(), "execute failed: {:?}", res.err());
    }
}
