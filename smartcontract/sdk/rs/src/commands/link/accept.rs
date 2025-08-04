use crate::{
    commands::{
        device::get::GetDeviceCommand, globalstate::get::GetGlobalStateCommand,
        link::get::GetLinkCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::link::accept::LinkAcceptArgs,
    state::link::LinkStatus,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct AcceptLinkCommand {
    pub link_pubkey: Pubkey,
    pub side_z_iface_name: String,
}

impl AcceptLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.link_pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        if link.status != LinkStatus::Requested {
            return Err(eyre::eyre!("Link is not in Requested status"));
        }

        let (_, device_z) = GetDeviceCommand {
            pubkey_or_code: link.side_z_pk.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device Z not found"))?;

        client.execute_transaction(
            DoubleZeroInstruction::AcceptLink(LinkAcceptArgs {
                side_z_iface_name: self.side_z_iface_name.clone(),
            }),
            vec![
                AccountMeta::new(self.link_pubkey, false),
                AccountMeta::new(device_z.contributor_pk, false),
                AccountMeta::new(link.side_z_pk, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}
