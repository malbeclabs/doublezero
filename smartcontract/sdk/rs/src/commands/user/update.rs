use double_zero_sla_program::{
    instructions::DoubleZeroInstruction,
    pda::get_user_pda,
    processors::user::update::UserUpdateArgs,
    state::user::{UserCYOA, UserType},
    types::{IpV4, NetworkV4},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

pub struct UpdateUserCommand {
    pub index: u128,
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub client_ip: Option<IpV4>,
    pub dz_ip: Option<IpV4>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
}

impl UpdateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_user_pda(&client.get_program_id(), self.index);
        client
            .execute_transaction(
                DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                    index: self.index,
                    user_type: self.user_type,
                    cyoa_type: self.cyoa_type,
                    client_ip: self.client_ip,
                    dz_ip: self.dz_ip,
                    tunnel_id: self.tunnel_id,
                    tunnel_net: self.tunnel_net,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
