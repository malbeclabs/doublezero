use crate::{
    commands::{
        globalstate::get::GetGlobalStateCommand,
        multicastgroup::subscribe::SubscribeMulticastGroupCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_accesspass_pda,
    processors::user::delete::UserDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteUserCommand {
    pub pubkey: Pubkey,
}

impl DeleteUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let user = client
            .get(self.pubkey)
            .map_err(|_| eyre::eyre!("User not found ({})", self.pubkey))?
            .get_user()
            .map_err(|e| eyre::eyre!(e))?;

        for mgroup_pk in user.publishers.iter().chain(user.subscribers.iter()) {
            SubscribeMulticastGroupCommand {
                group_pk: *mgroup_pk,
                user_pk: self.pubkey,
                client_ip: user.client_ip,
                publisher: false,
                subscriber: false,
            }
            .execute(client)?;
        }

        let (accesspass_pk, _) =
            get_accesspass_pda(&client.get_program_id(), &user.client_ip, &user.owner);
        client.execute_transaction(
            DoubleZeroInstruction::DeleteUser(UserDeleteArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(accesspass_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
