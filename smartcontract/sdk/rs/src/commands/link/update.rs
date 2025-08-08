use crate::{commands::link::get::GetLinkCommand, DoubleZeroClient, GetGlobalStateCommand};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::link::update::LinkUpdateArgs,
    state::link::LinkLinkType,
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
}

impl UpdateLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        client.execute_transaction(
            DoubleZeroInstruction::UpdateLink(LinkUpdateArgs {
                code: self.code.clone(),
                contributor_pk: self.contributor_pk,
                tunnel_type: self.tunnel_type,
                bandwidth: self.bandwidth,
                mtu: self.mtu,
                delay_ns: self.delay_ns,
                jitter_ns: self.jitter_ns,
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(link.contributor_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
