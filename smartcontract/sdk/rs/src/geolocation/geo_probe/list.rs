use std::collections::HashMap;

use doublezero_geolocation::state::{accounttype::AccountType, geo_probe::GeoProbe};
use solana_rpc_client_api::{
    config::RpcProgramAccountsConfig,
    filter::{Memcmp, MemcmpEncodedBytes, RpcFilterType},
};
use solana_sdk::pubkey::Pubkey;

use crate::geolocation::client::GeolocationClient;

#[derive(Debug, PartialEq, Clone)]
pub struct ListGeoProbeCommand;

impl ListGeoProbeCommand {
    pub fn execute(
        &self,
        client: &dyn GeolocationClient,
    ) -> eyre::Result<HashMap<Pubkey, GeoProbe>> {
        let program_id = client.get_program_id();
        let filters = vec![RpcFilterType::Memcmp(Memcmp::new(
            0,
            MemcmpEncodedBytes::Bytes(vec![AccountType::GeoProbe as u8]),
        ))];

        let accounts = client.get_program_accounts(
            &program_id,
            RpcProgramAccountsConfig {
                filters: Some(filters),
                ..Default::default()
            },
        )?;

        accounts
            .into_iter()
            .map(|(pubkey, account)| {
                let probe = GeoProbe::try_from(&account.data[..])
                    .map_err(|_| eyre::eyre!("Failed to deserialize GeoProbe account {pubkey}"))?;
                Ok((pubkey, probe))
            })
            .collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::geolocation::client::MockGeolocationClient;
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
    fn test_list_geo_probes() {
        let mut client = MockGeolocationClient::new();
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        let probe1 = make_geo_probe("probe-ams");
        let probe2 = make_geo_probe("probe-fra");
        let pk1 = Pubkey::new_unique();
        let pk2 = Pubkey::new_unique();

        let accounts = vec![
            (
                pk1,
                Account {
                    data: borsh::to_vec(&probe1).unwrap(),
                    owner: program_id,
                    ..Account::default()
                },
            ),
            (
                pk2,
                Account {
                    data: borsh::to_vec(&probe2).unwrap(),
                    owner: program_id,
                    ..Account::default()
                },
            ),
        ];

        client
            .expect_get_program_accounts()
            .returning(move |_, _| Ok(accounts.clone()));

        let cmd = ListGeoProbeCommand;
        let result = cmd.execute(&client);
        assert!(result.is_ok());
        let probes = result.unwrap();
        assert_eq!(probes.len(), 2);
        assert_eq!(probes[&pk1].code, "probe-ams");
        assert_eq!(probes[&pk2].code, "probe-fra");
    }

    #[test]
    fn test_list_geo_probes_empty() {
        let mut client = MockGeolocationClient::new();
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        client
            .expect_get_program_accounts()
            .returning(|_, _| Ok(vec![]));

        let cmd = ListGeoProbeCommand;
        let result = cmd.execute(&client);
        assert!(result.is_ok());
        assert!(result.unwrap().is_empty());
    }
}
