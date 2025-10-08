use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_user_pda,
    processors::user::create_subscribe::UserCreateSubscribeArgs,
    state::{
        multicastgroup::MulticastGroupStatus,
        user::{UserCYOA, UserType},
    },
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};
use std::net::Ipv4Addr;

use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, globalstate::get::GetGlobalStateCommand,
        multicastgroup::get::GetMulticastGroupCommand,
    },
    DoubleZeroClient,
};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateSubscribeUserCommand {
    pub user_type: UserType,
    pub device_pk: Pubkey,
    pub cyoa_type: UserCYOA,
    pub client_ip: Ipv4Addr,
    pub mgroup_pk: Pubkey,
    pub publisher: bool,
    pub subscriber: bool,
}

impl CreateSubscribeUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, mgroup) = GetMulticastGroupCommand {
            pubkey_or_code: self.mgroup_pk.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("MulticastGroup not found"))?;

        if mgroup.status != MulticastGroupStatus::Activated {
            eyre::bail!("MulticastGroup not active");
        }

        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: client.get_payer(),
        }
        .execute(client)
        .or_else(|_| {
            GetAccessPassCommand {
                client_ip: self.client_ip,
                user_payer: client.get_payer(),
            }
            .execute(client)
        })
        .map_err(|_| eyre::eyre!("You have no Access Pass"))?;

        let (pda_pubkey, _) = get_user_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
                    user_type: self.user_type,
                    cyoa_type: self.cyoa_type,
                    client_ip: self.client_ip,
                    publisher: self.publisher,
                    subscriber: self.subscriber,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.device_pk, false),
                    AccountMeta::new(self.mgroup_pk, false),
                    AccountMeta::new(accesspass_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
