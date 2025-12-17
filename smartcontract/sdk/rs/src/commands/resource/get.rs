use doublezero_serviceability::{
    pda::get_resource_extension_pda,
    resource::ResourceBlockType,
    state::{accountdata::AccountData, resource_extension::ResourceExtensionOwned},
};
use eyre::eyre;
use solana_sdk::pubkey::Pubkey;

use crate::DoubleZeroClient;

#[derive(Default, Debug, PartialEq, Clone)]
pub struct GetResourceCommand {
    pub resource_block_type: ResourceBlockType,
}

impl GetResourceCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<(Pubkey, ResourceExtensionOwned)> {
        let (pubkey, _, _) =
            get_resource_extension_pda(&client.get_program_id(), self.resource_block_type);

        match client.get(pubkey)? {
            AccountData::ResourceExtension(resource_extension) => Ok((pubkey, resource_extension)),
            _ => Err(eyre!("Invalid resource extension")),
        }
    }
}
