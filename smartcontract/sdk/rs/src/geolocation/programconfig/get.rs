use doublezero_geolocation::{pda, state::program_config::GeolocationProgramConfig};
use solana_sdk::pubkey::Pubkey;

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct GetProgramConfigCommand;

impl GetProgramConfigCommand {
    pub fn execute(
        &self,
        client: &dyn GeolocationClient,
    ) -> eyre::Result<(Pubkey, GeolocationProgramConfig)> {
        let program_id = client.get_program_id();
        let (config_pda, _) = pda::get_program_config_pda(&program_id);

        let account = client.get_account(config_pda)?;
        let config = GeolocationProgramConfig::try_from(&account.data[..])
            .map_err(|_| eyre::eyre!("Failed to deserialize GeolocationProgramConfig account"))?;

        Ok((config_pda, config))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use doublezero_geolocation::state::accounttype::AccountType;
    use solana_sdk::account::Account;

    #[test]
    fn test_get_program_config() {
        let mut client = MockGeolocationClient::new();
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let (config_pda, _) = pda::get_program_config_pda(&program_id);

        let config = GeolocationProgramConfig {
            account_type: AccountType::ProgramConfig,
            bump_seed: 255,
            version: 1,
            min_compatible_version: 1,
            serviceability_program_id: Pubkey::new_unique(),
        };

        let data = borsh::to_vec(&config).unwrap();
        let svc_program_id = config.serviceability_program_id;

        client
            .expect_get_account()
            .withf(move |pk| *pk == config_pda)
            .returning(move |_| {
                Ok(Account {
                    data: data.clone(),
                    owner: program_id,
                    ..Account::default()
                })
            });

        let cmd = GetProgramConfigCommand;
        let result = cmd.execute(&client);
        assert!(result.is_ok());
        let (pk, returned_config) = result.unwrap();
        assert_eq!(pk, config_pda);
        assert_eq!(returned_config.serviceability_program_id, svc_program_id);
    }
}
