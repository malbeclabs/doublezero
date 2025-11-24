use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, globalstate::get::GetGlobalStateCommand,
        user::get::GetUserCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::user::resume::UserResumeArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct ResumeUserCommand {
    pub pubkey: Pubkey,
}

impl ResumeUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, user) = GetUserCommand {
            pubkey: self.pubkey,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("User not found"))?;

        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: std::net::Ipv4Addr::UNSPECIFIED,
            user_payer: user.owner,
        }
        .execute(client)
        .or_else(|_| {
            GetAccessPassCommand {
                client_ip: user.client_ip,
                user_payer: user.owner,
            }
            .execute(client)
        })?;

        client.execute_transaction(
            DoubleZeroInstruction::ResumeUser(UserResumeArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(accesspass_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
