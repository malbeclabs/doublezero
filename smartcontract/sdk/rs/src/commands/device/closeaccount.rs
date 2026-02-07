use crate::{
    commands::{
        device::get::GetDeviceCommand, globalconfig::get::GetGlobalConfigCommand,
        globalstate::get::GetGlobalStateCommand, resource::get::GetResourceCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::device::closeaccount::DeviceCloseAccountArgs,
    resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CloseAccountDeviceCommand {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
}

impl CloseAccountDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (globalconfig_pubkey, _globalconfig) = GetGlobalConfigCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalconfig not initialized"))?;

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device not found"))?;

        if device.reference_count > 0 {
            return Err(eyre::eyre!(
                "Device cannot be closed, it has {} references",
                device.reference_count
            ));
        }

        // Close TunnelIds and DzPrefixBlock resources
        let mut resource_accounts = vec![];
        let mut owner_accounts = vec![];
        for idx in 0..device.dz_prefixes.len() + 1 {
            let resource_type = match idx {
                0 => ResourceType::TunnelIds(self.pubkey, 0),
                _ => ResourceType::DzPrefixBlock(self.pubkey, idx - 1),
            };
            let (pda, resource) = GetResourceCommand { resource_type }
                .execute(client)
                .map_err(|_err| eyre::eyre!("Resource {:?} not found", resource_type))?;
            resource_accounts.push(AccountMeta::new(pda, false));
            owner_accounts.push(AccountMeta::new(resource.owner, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::CloseAccountDevice(DeviceCloseAccountArgs {
                resource_count: resource_accounts.len(),
            }),
            [
                vec![
                    AccountMeta::new(self.pubkey, false),
                    AccountMeta::new(self.owner, false),
                    AccountMeta::new(device.contributor_pk, false),
                    AccountMeta::new(device.location_pk, false),
                    AccountMeta::new(device.exchange_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(globalconfig_pubkey, false),
                ],
                resource_accounts,
                owner_accounts,
            ]
            .concat(),
        )
    }
}
