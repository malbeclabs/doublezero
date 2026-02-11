use std::net::Ipv4Addr;

use crate::DoubleZeroClient;
use doublezero_serviceability::{
    pda::get_accesspass_pda,
    state::{accesspass::AccessPass, accountdata::AccountData},
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetAccessPassCommand {
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl GetAccessPassCommand {
    pub fn execute(
        &self,
        client: &dyn DoubleZeroClient,
    ) -> eyre::Result<Option<(Pubkey, AccessPass)>> {
        let (pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &self.client_ip, &self.user_payer);

        match client.get(pubkey) {
            Ok(AccountData::AccessPass(accesspass)) => Ok(Some((pubkey, accesspass))),
            Ok(_) | Err(_) => Ok(None),
        }
    }
}
