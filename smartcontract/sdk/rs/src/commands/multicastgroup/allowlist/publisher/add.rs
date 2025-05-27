use doublezero_sla_program::{
    instructions::DoubleZeroInstruction,
    processors::multicastgroup::allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::multicastgroup::get::GetMulticastGroupCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct AddMulticastGroupPubAllowlistCommand {
    pub pubkey_or_code: String,
    pub pubkey: Pubkey,
}

impl AddMulticastGroupPubAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey_or_code.clone(),
        }
        .execute(client)?;

        if mgroup.pub_allowlist.contains(&self.pubkey) {
            return Err(eyre::eyre!("Publisher is already in the allowlist"));
        }

        client.execute_transaction(
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
                AddMulticastGroupPubAllowlistArgs {
                    pubkey: self.pubkey,
                },
            ),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
