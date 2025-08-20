use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::user::activate::UserActivateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateUserCommand {
    pub user_pubkey: Pubkey,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub dz_ip: Ipv4Addr,
    pub validator_pubkey: Option<Pubkey>,
}

impl ActivateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
                dz_ip: self.dz_ip,
                validator_pubkey: self.validator_pubkey,
            }),
            vec![
                AccountMeta::new(self.user_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
