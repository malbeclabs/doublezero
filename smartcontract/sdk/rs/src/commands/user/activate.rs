use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_user_pda,
    processors::user::activate::UserActivateArgs,
    types::{IpV4, NetworkV4},
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct ActivateUserCommand {
    pub index: u128,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub dz_ip: IpV4,
}

impl ActivateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) = get_user_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                index: self.index,
                bump_seed,
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
                dz_ip: self.dz_ip,
            }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
