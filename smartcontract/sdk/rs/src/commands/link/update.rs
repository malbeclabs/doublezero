use crate::{commands::link::get::GetLinkCommand, DoubleZeroClient, GetGlobalStateCommand};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::link::update::LinkUpdateArgs,
    state::link::{LinkDesiredStatus, LinkLinkType, LinkStatus},
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
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(link.contributor_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
