use doublezero_sla_program::{
    instructions::DoubleZeroInstruction,
    processors::multicastgroup::allowlist::publisher::remove::RemoveMulticastGroupPubAllowlistArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::multicastgroup::get::GetMulticastGroupCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct RemoveMulticastGroupPubAllowlistCommand {
    pub pubkey_or_code: String,
    pub pubkey: Pubkey,
}

impl RemoveMulticastGroupPubAllowlistCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (pda_pubkey, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey_or_code.clone(),
        }
        .execute(client)?;

        if !mgroup.pub_allowlist.contains(&self.pubkey) {
            return Err(eyre::eyre!("Publisher is not in the allowlist"));
        }

        client.execute_transaction(
            DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(
                RemoveMulticastGroupPubAllowlistArgs {
                    pubkey: self.pubkey,
                },
            ),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }
}
