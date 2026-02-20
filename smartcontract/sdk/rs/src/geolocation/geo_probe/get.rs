use doublezero_geolocation::{pda, state::geo_probe::GeoProbe};
use solana_sdk::pubkey::Pubkey;
use std::str::FromStr;

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct GetGeoProbeCommand {
    pub pubkey_or_code: String,
}

impl GetGeoProbeCommand {
    pub fn execute(&self, client: &dyn GeolocationClient) -> eyre::Result<(Pubkey, GeoProbe)> {
        let program_id = client.get_program_id();

        let pubkey = match Pubkey::from_str(&self.pubkey_or_code) {
            Ok(pk) => pk,
            Err(_) => {
                let (pda, _) = pda::get_geo_probe_pda(&program_id, &self.pubkey_or_code);
                pda
            }
        };

        let account = client.get_account(pubkey)?;
        let probe = GeoProbe::try_from(&account.data[..])
            .map_err(|_| eyre::eyre!("Failed to deserialize GeoProbe account"))?;

        Ok((pubkey, probe))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
    use doublezero_geolocation::state::accounttype::AccountType;
    use solana_sdk::account::Account;

    fn make_geo_probe(code: &str) -> GeoProbe {
        GeoProbe {
            account_type: AccountType::GeoProbe,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            exchange_pk: Pubkey::new_unique(),
            public_ip: std::net::Ipv4Addr::new(1, 2, 3, 4),
            location_offset_port: 8923,
            code: code.to_string(),
            parent_devices: vec![],
            metrics_publisher_pk: Pubkey::new_unique(),
            reference_count: 0,
        }
    }

    #[test]
    fn test_get_geo_probe_by_code() {
        let mut client = MockGeolocationClient::new();
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let code = "probe-ams";
        let probe = make_geo_probe(code);
        let (expected_pda, _) = pda::get_geo_probe_pda(&program_id, code);

        client
            .expect_get_account()
            .withf(move |pk| *pk == expected_pda)
            .returning(move |_| {
                Ok(Account {
                    data: borsh::to_vec(&probe.clone()).unwrap(),
                    owner: program_id,
                    ..Account::default()
                })
            });

        let cmd = GetGeoProbeCommand {
            pubkey_or_code: code.to_string(),
        };
        let result = cmd.execute(&client);
        assert!(result.is_ok());
        let (pk, returned_probe) = result.unwrap();
        assert_eq!(pk, expected_pda);
        assert_eq!(returned_probe.code, code);
    }

    #[test]
    fn test_get_geo_probe_by_pubkey() {
        let mut client = MockGeolocationClient::new();
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let probe_pk = Pubkey::new_unique();
        let probe = make_geo_probe("probe-fra");

        client
            .expect_get_account()
            .withf(move |pk| *pk == probe_pk)
            .returning(move |_| {
                Ok(Account {
                    data: borsh::to_vec(&probe.clone()).unwrap(),
                    owner: program_id,
                    ..Account::default()
                })
            });

        let cmd = GetGeoProbeCommand {
            pubkey_or_code: probe_pk.to_string(),
        };
        let result = cmd.execute(&client);
        assert!(result.is_ok());
        let (pk, returned_probe) = result.unwrap();
        assert_eq!(pk, probe_pk);
        assert_eq!(returned_probe.code, "probe-fra");
    }
}
