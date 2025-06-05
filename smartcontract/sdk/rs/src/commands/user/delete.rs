use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_user_pda,
    processors::user::delete::UserDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{
    commands::{
        globalstate::get::GetGlobalStateCommand,
        multicastgroup::subscribe::SubscribeMulticastGroupCommand,
    },
    DoubleZeroClient,
};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteUserCommand {
    pub index: u128,
}

impl DeleteUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (user_pubkey, bump_seed) = get_user_pda(&client.get_program_id(), self.index);

        let user = client
            .get(user_pubkey)
            .map_err(|_| eyre::eyre!("User not found ({})", user_pubkey))?
            .get_user();

        for mgroup_pk in user.publishers.iter().chain(user.subscribers.iter()) {
            SubscribeMulticastGroupCommand {
                group_pk: *mgroup_pk,
                user_pk: user_pubkey,
                publisher: false,
                subscriber: false,
            }
            .execute(client)?;
        }

        client.execute_transaction(
            DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
                index: self.index,
                bump_seed,
            }),
            vec![
                AccountMeta::new(user_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
