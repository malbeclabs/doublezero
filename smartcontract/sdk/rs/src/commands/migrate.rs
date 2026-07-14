use crate::DoubleZeroClient;
use doublezero_serviceability::{
    pda::{get_user_old_pda, get_user_pda},
    processors::migrate::MigrateArgs,
    state::{accountdata::AccountData, accounttype::AccountType, user::User},
};
use doublezero_serviceability_instruction::migrate::migrate;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::collections::HashMap;

#[derive(Debug, PartialEq, Clone)]
pub struct MigrateCommand {}

impl MigrateCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Vec<Signature>> {
        let program_id = client.get_program_id();

        let users: HashMap<Pubkey, User> = client
            .gets(AccountType::User)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::User(user) => Ok((k, user)),
                _ => eyre::bail!("Invalid Account Type"),
            })
            .collect::<eyre::Result<HashMap<Pubkey, User>>>()?;

        let mut signatures = Vec::new();
        for (pubkey, user) in users.into_iter() {
            let (old_pubkey, _old_bump_seed) = get_user_old_pda(&program_id, user.index);

            if pubkey == old_pubkey {
                let (new_pubkey, _new_bump_seed) =
                    get_user_pda(&program_id, &user.client_ip, user.user_type);

                let signature = client.send_transaction(migrate(
                    &program_id,
                    &client.get_payer(),
                    &old_pubkey,
                    &new_pubkey,
                    MigrateArgs {},
                ))?;

                println!(
                    "Migrated user from {} to {}: {:?}",
                    old_pubkey, new_pubkey, signature
                );
                signatures.push(signature);
            }
        }

        Ok(signatures)
    }
}
