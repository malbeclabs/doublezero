use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, globalstate::get::GetGlobalStateCommand,
        user::get::GetUserCommand,
    },
    DoubleZeroClient,
};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::user::activate::UserActivateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateUserCommand {
    pub user_pubkey: Pubkey,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub dz_ip: Ipv4Addr,
}

impl ActivateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, user) = GetUserCommand {
            pubkey: self.user_pubkey,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("User not found"))?;

        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: user.owner,
        }
        .execute(client)
        .or_else(|_| {
            GetAccessPassCommand {
                client_ip: user.client_ip,
                user_payer: user.owner,
            }
            .execute(client)
        })
        .map_err(|_| eyre::eyre!("You have no Access Pass"))?;

        client.execute_transaction(
            DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
                dz_ip: self.dz_ip,
            }),
            vec![
                AccountMeta::new(self.user_pubkey, false),
                AccountMeta::new(accesspass_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
