use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_accesspass_pda,
    processors::user::activate::UserActivateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, user::get::GetUserCommand},
    DoubleZeroClient,
};

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

        let (accesspass_pk, _) =
            get_accesspass_pda(&client.get_program_id(), &user.client_ip, &user.owner);

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
