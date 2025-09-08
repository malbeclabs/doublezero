use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, link::get::GetLinkCommand},
    DoubleZeroClient,
};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::link::activate::LinkActivateArgs,
    state::link::LinkStatus,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateLinkCommand {
    pub link_pubkey: Pubkey,
    pub side_a_pk: Pubkey,
    pub side_z_pk: Pubkey,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
}

impl ActivateLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.link_pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        if link.status != LinkStatus::Pending {
            return Err(eyre::eyre!("Link is not in Pending status"));
        }

        client.execute_transaction(
            DoubleZeroInstruction::ActivateLink(LinkActivateArgs {
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
            }),
            vec![
                AccountMeta::new(self.link_pubkey, false),
                AccountMeta::new(self.side_a_pk, false),
                AccountMeta::new(self.side_z_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
