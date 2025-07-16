use crate::{index::nextindex, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalconfig_pda, get_user_pda},
    processors::user::create::UserCreateArgs,
    state::user::{UserCYOA, UserType},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct CreateUserCommand {
    pub user_type: UserType,
    pub device_pk: Pubkey,
    pub cyoa_type: UserCYOA,
    pub client_ip: Ipv4Addr,
}

impl CreateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let index = nextindex();
        let (globalstate_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_user_pda(&client.get_program_id(), index);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateUser(UserCreateArgs {
                    index,
                    bump_seed,
                    user_type: self.user_type,
                    device_pk: self.device_pk,
                    cyoa_type: self.cyoa_type,
                    client_ip: self.client_ip,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.device_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
