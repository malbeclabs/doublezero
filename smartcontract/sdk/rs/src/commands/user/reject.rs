use double_zero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_user_pda,
    processors::user::reject::UserRejectArgs, 
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::accountdata::getglobalstate::GetGlobalStateCommand, DoubleZeroClient};

pub struct RejectUserCommand {
    pub index: u128,
    pub reason: String,
}

impl RejectUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_user_pda(&client.get_program_id(), self.index);
        client
            .execute_transaction(
                DoubleZeroInstruction::RejectUser(UserRejectArgs {
                    index: self.index,
                    reason: self.reason.clone(),
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            
    }
}
