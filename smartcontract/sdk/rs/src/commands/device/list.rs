use crate::DoubleZeroClient;
use doublezero_sla_program::state::{
    accountdata::AccountData, accounttype::AccountType, device::Device,
};
use solana_sdk::pubkey::Pubkey;

pub struct ListDeviceCommand {}

impl ListDeviceCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<(Pubkey, Device)>> {
        let mut sorted_device_list: Vec<(Pubkey, Device)> = client
            .gets(AccountType::Device)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Device(device) => (k, device),
                _ => panic!("Invalid Account Type"),
            })
            .collect();


        sorted_device_list.sort_by(|(_, a), (_, b)| {
              a.code.cmp(&b.code)
          });


        Ok(sorted_device_list)
    }
}
