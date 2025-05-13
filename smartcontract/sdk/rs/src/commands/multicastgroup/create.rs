use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_multicastgroup_pda,
    processors::multicastgroup::create::MulticastGroupCreateArgs, types::IpV4,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateMulticastGroupCommand {
    pub code: String,
    pub multicast_ip: IpV4,
    pub max_bandwidth: u64,
    pub owner: Pubkey,
}

impl CreateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) =
            get_multicastgroup_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                    index: globalstate.account_index + 1,
                    bump_seed,
                    code: self.code.to_string(),
                    multicast_ip: self.multicast_ip,
                    max_bandwidth: self.max_bandwidth,
                    owner: self.owner,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
