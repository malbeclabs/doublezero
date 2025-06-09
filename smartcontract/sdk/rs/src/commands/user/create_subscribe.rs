use doublezero_sla_program::{
    instructions::DoubleZeroInstruction,
    pda::get_user_pda,
    processors::user::create_subscribe::UserCreateSubscribeArgs,
    state::{
        multicastgroup::MulticastGroupStatus,
        user::{UserCYOA, UserType},
    },
    types::IpV4,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{
    commands::{
        globalstate::get::GetGlobalStateCommand, multicastgroup::get::GetMulticastGroupCommand,
    },
    DoubleZeroClient,
};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateSubscribeUserCommand {
    pub user_type: UserType,
    pub device_pk: Pubkey,
    pub cyoa_type: UserCYOA,
    pub client_ip: IpV4,
    pub mgroup_pk: Pubkey,
    pub publisher: bool,
    pub subscriber: bool,
}

impl CreateSubscribeUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand {}
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
        if self.publisher && !mgroup.pub_allowlist.contains(&client.get_payer()) {
            eyre::bail!("Publisher not allowed");
        }
        if self.subscriber && !mgroup.sub_allowlist.contains(&client.get_payer()) {
            eyre::bail!("Subscriber not allowed");
        }

        let (pda_pubkey, bump_seed) =
            get_user_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
                    index: globalstate.account_index + 1,
                    bump_seed,
                    user_type: self.user_type,
                    device_pk: self.device_pk,
                    cyoa_type: self.cyoa_type,
                    client_ip: self.client_ip,
                    publisher: self.publisher,
                    subscriber: self.subscriber,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.device_pk, false),
                    AccountMeta::new(self.mgroup_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
