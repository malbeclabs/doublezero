use crate::{commands::link::get::GetLinkCommand, DoubleZeroClient, GetGlobalStateCommand};
use doublezero_program_common::{types::NetworkV4, validate_account_code};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_resource_extension_pda,
    processors::link::update::LinkUpdateArgs,
    resource::ResourceType,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        link::{LinkDesiredStatus, LinkLinkType, LinkStatus},
    },
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateLinkCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub contributor_pk: Option<Pubkey>,
    pub tunnel_type: Option<LinkLinkType>,
    pub bandwidth: Option<u64>,
    pub mtu: Option<u32>,
    pub delay_ns: Option<u64>,
    pub jitter_ns: Option<u64>,
    pub delay_override_ns: Option<u64>,
    pub status: Option<LinkStatus>,
    pub desired_status: Option<LinkDesiredStatus>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
    pub link_topologies: Option<Vec<Pubkey>>,
    pub unicast_drained: Option<bool>,
}

impl UpdateLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        let code = self
            .code
            .as_ref()
            .map(|code| {
                validate_account_code(code).map(|mut c| {
                    c.make_ascii_lowercase();
                    c
                })
            })
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let updating_tunnel_resources = self.tunnel_id.is_some() || self.tunnel_net.is_some();

        let use_onchain_allocation =
            is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation)
                && updating_tunnel_resources;

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(link.contributor_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        // Device accounts needed when updating tunnel_net (for interface IP update)
        if self.tunnel_net.is_some() {
            accounts.push(AccountMeta::new(link.side_a_pk, false));
            accounts.push(AccountMeta::new(link.side_z_pk, false));
        }

        if use_onchain_allocation {
            // DeviceTunnelBlock (global)
            let (device_tunnel_block_ext, _, _) = get_resource_extension_pda(
                &client.get_program_id(),
                ResourceType::DeviceTunnelBlock,
            );
            accounts.push(AccountMeta::new(device_tunnel_block_ext, false));

            // LinkIds (global)
            let (link_ids_ext, _, _) =
                get_resource_extension_pda(&client.get_program_id(), ResourceType::LinkIds);
            accounts.push(AccountMeta::new(link_ids_ext, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
                code,
                contributor_pk: self.contributor_pk,
                tunnel_type: self.tunnel_type,
                bandwidth: self.bandwidth,
                mtu: self.mtu,
                delay_ns: self.delay_ns,
                jitter_ns: self.jitter_ns,
                delay_override_ns: self.delay_override_ns,
                status: self.status,
                desired_status: self.desired_status,
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
                use_onchain_allocation,
                link_topologies: self.link_topologies.clone(),
                unicast_drained: self.unicast_drained,
            }),
            accounts,
        )
    }
}
