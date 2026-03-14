use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::user::ban::UserBanArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct BanUserCommand {
    pub pubkey: Pubkey,
}

impl BanUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        self.execute_inner(client, false)
    }

    pub fn execute_quiet(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        self.execute_inner(client, true)
    }

    fn execute_inner(&self, client: &dyn DoubleZeroClient, quiet: bool) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if quiet {
            client.execute_authorized_transaction_quiet(
                DoubleZeroInstruction::BanUser(UserBanArgs {}),
                accounts,
            )
        } else {
            client.execute_authorized_transaction(
                DoubleZeroInstruction::BanUser(UserBanArgs {}),
                accounts,
            )
        }
    }
}
