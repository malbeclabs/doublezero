use crate::{
    commands::{accesspass::get::GetAccessPassCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_user_pda,
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
    pub tunnel_endpoint: Ipv4Addr,
    pub tenant_pk: Option<Pubkey>,
}

impl CreateUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        // First try to get AccessPass for the client IP
        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: client.get_payer(),
        }
        .execute(client)?
        .or_else(|| {
            GetAccessPassCommand {
                client_ip: Ipv4Addr::UNSPECIFIED,
                user_payer: client.get_payer(),
            }
            .execute(client)
            .ok()
            .flatten()
        })
        .ok_or_else(|| eyre::eyre!("You have no Access Pass"))?;

        let (pda_pubkey, _) =
            get_user_pda(&client.get_program_id(), &self.client_ip, self.user_type);

        let mut accounts = vec![
            AccountMeta::new(pda_pubkey, false),
            AccountMeta::new(self.device_pk, false),
            AccountMeta::new(accesspass_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        // Add tenant account if provided and not default
        if let Some(tenant_pk) = self.tenant_pk {
            if tenant_pk != Pubkey::default() {
                accounts.push(AccountMeta::new(tenant_pk, false));
            }
        }

        client
            .execute_transaction(
                DoubleZeroInstruction::CreateUser(UserCreateArgs {
                    user_type: self.user_type,
                    cyoa_type: self.cyoa_type,
                    client_ip: self.client_ip,
                    tunnel_endpoint: self.tunnel_endpoint,
                }),
                accounts,
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
