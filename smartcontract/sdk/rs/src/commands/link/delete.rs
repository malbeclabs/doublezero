use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, link::get::GetLinkCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::link::delete::LinkDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteLinkCommand {
    pub pubkey: Pubkey,
}

impl DeleteLinkCommand {
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
            DoubleZeroInstruction::DeleteLink(LinkDeleteArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(link.contributor_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
