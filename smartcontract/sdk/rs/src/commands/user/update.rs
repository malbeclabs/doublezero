use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::user::update::UserUpdateArgs,
    state::user::{UserCYOA, UserType},
    types::NetworkV4,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateUserCommand {
    pub pubkey: Pubkey,
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub client_ip: Option<Ipv4Addr>,
    pub dz_ip: Option<Ipv4Addr>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
}

impl UpdateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                user_type: self.user_type,
                cyoa_type: self.cyoa_type,
                client_ip: self.client_ip,
                dz_ip: self.dz_ip,
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
            }),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
