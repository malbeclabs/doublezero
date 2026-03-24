use crate::{
    commands::{accesspass::get::GetAccessPassCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::user::transfer_ownership::TransferUserOwnershipArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct TransferUserOwnershipCommand {
    pub user_pubkey: Pubkey,
    pub client_ip: Ipv4Addr,
    pub old_user_payer: Pubkey,
    pub new_user_payer: Pubkey,
}

impl TransferUserOwnershipCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        // Resolve old access pass (current owner's)
        let (old_accesspass_pk, _) = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: self.old_user_payer,
        }
        .execute(client)?
        .ok_or_else(|| {
            eyre::eyre!(
                "Old AccessPass not found for IP {} and payer {}",
                self.client_ip,
                self.old_user_payer
            )
        })?;

        // Resolve new access pass (new owner's)
        let (new_accesspass_pk, _) = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: self.new_user_payer,
        }
        .execute(client)?
        .ok_or_else(|| {
            eyre::eyre!(
                "New AccessPass not found for IP {} and payer {}",
                self.client_ip,
                self.new_user_payer
            )
        })?;

        let accounts = vec![
            AccountMeta::new(self.user_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(old_accesspass_pk, false),
            AccountMeta::new(new_accesspass_pk, false),
        ];

        client.execute_transaction(
            DoubleZeroInstruction::TransferUserOwnership(TransferUserOwnershipArgs {}),
            accounts,
        )
    }
}
