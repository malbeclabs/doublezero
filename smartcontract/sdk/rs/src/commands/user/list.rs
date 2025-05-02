use crate::DoubleZeroClient;
use doublezero_sla_program::state::{
    accountdata::AccountData, accounttype::AccountType, user::User,
};
use solana_sdk::pubkey::Pubkey;

pub struct ListUserCommand {}

impl ListUserCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<(Pubkey, User)>> {
        let mut sorted_user_list: Vec<(Pubkey, User)> = client
            .gets(AccountType::User)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::User(user) => (k, user),
                _ => panic!("Invalid Account Type"),
            })
            .collect();

        sorted_user_list.sort_by(|(_, a), (_, b)| {
              a.device_pk
                  .cmp(&b.device_pk)
                  .then_with(|| a.tunnel_id.cmp(&b.tunnel_id))
          });


        Ok(sorted_user_list)
    }
}
