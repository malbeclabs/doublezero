use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_user_pda,
    processors::user::suspend::UserSuspendArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct SuspendUserCommand {
    pub index: u128,
}

impl SuspendUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) = get_user_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::SuspendUser(UserSuspendArgs {
                index: self.index,
                bump_seed,
            }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
