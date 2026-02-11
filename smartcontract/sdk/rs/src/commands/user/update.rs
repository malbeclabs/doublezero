use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, user::get::GetUserCommand},
    DoubleZeroClient,
};
use doublezero_program_common::types::NetworkV4;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::user::update::UserUpdateArgs,
    state::user::{UserCYOA, UserType},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateUserCommand {
    pub pubkey: Pubkey,
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub dz_ip: Option<Ipv4Addr>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
    pub validator_pubkey: Option<Pubkey>,
    pub tenant_pk: Option<Pubkey>,
}

impl UpdateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        // If updating tenant_pk, add old and new tenant accounts for reference counting
        if let Some(new_tenant_pk) = self.tenant_pk {
            // Get current user to find old tenant
            let (_user_pubkey, user) = GetUserCommand {
                pubkey: self.pubkey,
            }
            .execute(client)?;

            let old_tenant_pk = user.tenant_pk;

            // Add tenant accounts (old_tenant, new_tenant)
            accounts.push(AccountMeta::new(old_tenant_pk, false));
            accounts.push(AccountMeta::new(new_tenant_pk, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                user_type: self.user_type,
                cyoa_type: self.cyoa_type,
                dz_ip: self.dz_ip,
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
                validator_pubkey: self.validator_pubkey,
                tenant_pk: self.tenant_pk,
            }),
            accounts,
        )
    }
}
