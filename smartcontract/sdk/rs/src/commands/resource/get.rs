use doublezero_serviceability::{
    pda::get_device_tunnel_block_pda,
    state::{accountdata::AccountData, resource_extension::ResourceExtensionOwned},
};
use eyre::eyre;
use solana_sdk::pubkey::Pubkey;

use crate::DoubleZeroClient;

#[derive(Default, Debug, PartialEq, Clone)]
pub struct GetResourceCommand {}

impl GetResourceCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<(Pubkey, ResourceExtensionOwned)> {
        let (pubkey, _) = get_device_tunnel_block_pda(&client.get_program_id());

        match client.get(pubkey)? {
            AccountData::ResourceExtension(resource_extension) => Ok((pubkey, resource_extension)),
            _ => Err(eyre!("Invalid resource extension")),
        }
    }
}
