use crate::{
    commands::{
        device::get::GetDeviceCommand, globalstate::get::GetGlobalStateCommand,
        link::get::GetLinkCommand,
    },
    DoubleZeroClient, LinkStatus,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::link::reject::LinkRejectArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct RejectLinkCommand {
    pub pubkey: Pubkey,
    pub reason: String,
}

impl RejectLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        let mut accounts = vec![
            AccountMeta::new(self.pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ];

        if link.status == LinkStatus::Requested {
            let (_, device_z) = GetDeviceCommand {
                pubkey_or_code: link.side_z_pk.to_string(),
            }
            .execute(client)
            .map_err(|_err| eyre::eyre!("Device Z not found"))?;

            accounts.push(AccountMeta::new(device_z.contributor_pk, false));
            accounts.push(AccountMeta::new(link.side_z_pk, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::RejectLink(LinkRejectArgs {
                reason: self.reason.clone(),
            }),
            accounts,
        )
    }
}
