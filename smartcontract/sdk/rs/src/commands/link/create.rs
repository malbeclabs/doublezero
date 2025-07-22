use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_link_pda,
    processors::link::create::LinkCreateArgs, state::link::LinkLinkType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateLinkCommand {
    pub code: String,
    pub side_a_pk: Pubkey,
    pub side_z_pk: Pubkey,
    pub link_type: LinkLinkType,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay_ns: u64,
    pub jitter_ns: u64,
}

impl CreateLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_link_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                    code: self.code.to_string(),
                    side_a_pk: self.side_a_pk,
                    side_z_pk: self.side_z_pk,
                    link_type: self.link_type,
                    bandwidth: self.bandwidth,
                    mtu: self.mtu,
                    delay_ns: self.delay_ns,
                    jitter_ns: self.jitter_ns,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.side_a_pk, false),
                    AccountMeta::new(self.side_z_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
