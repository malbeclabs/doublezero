use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_sdk::commands::location::get::GetLocationCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetLocationCliCommand {
    /// Location Pubkey or code to get details for
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
}

impl GetLocationCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, location) = client.get_location(GetLocationCommand {
            pubkey_or_code: self.code,
        })?;

        writeln!(out,
                "account: {},\r\ncode: {}\r\nname: {}\r\ncountry: {}\r\nlat: {}\r\nlng: {}\r\nloc_id: {}\r\nreference_count: {}\r\nstatus: {}\r\nowner: {}",
                pubkey,
                location.code,
                location.name,
                location.country,
                location.lat,
                location.lng,
                location.loc_id,
                location.reference_count,
                location.status,
                location.owner
            )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{location::get::GetLocationCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{AccountType, GetLocationCommand, Location, LocationStatus};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, str::FromStr};

    #[test]
    fn test_cli_location_get() {
        let mut client = create_test_client();

        let location1_pubkey =
            Pubkey::from_str("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB").unwrap();
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "Test Country".to_string(),
            lat: 12.34,
            lng: 56.78,
            loc_id: 1,
            status: LocationStatus::Activated,
            owner: location1_pubkey,
        };

        let location2 = location1.clone();
        client
            .expect_get_location()
            .with(predicate::eq(GetLocationCommand {
                pubkey_or_code: location1_pubkey.to_string(),
            }))
            .returning(move |_| Ok((location1_pubkey, location2.clone())));
        let location3 = location1.clone();
        client
            .expect_get_location()
            .with(predicate::eq(GetLocationCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((location1_pubkey, location3.clone())));
        client
            .expect_get_location()
            .returning(move |_| Err(eyre::eyre!("not found")));

        client.expect_list_location().returning(move |_| {
            let mut list = HashMap::new();
            list.insert(location1_pubkey, location1.clone());
            Ok(list)
        });

        /*****************************************************************************************************/
        // Expected failure
        let mut output = Vec::new();
        let res = GetLocationCliCommand {
            code: Pubkey::new_unique().to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success
        let mut output = Vec::new();
        let res = GetLocationCliCommand {
            code: location1_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB,\r\ncode: test\r\nname: Test Location\r\ncountry: Test Country\r\nlat: 12.34\r\nlng: 56.78\r\nloc_id: 1\r\nreference_count: 0\r\nstatus: activated\r\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\n");

        // Expected success
        let mut output = Vec::new();
        let res = GetLocationCliCommand {
            code: "test".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB,\r\ncode: test\r\nname: Test Location\r\ncountry: Test Country\r\nlat: 12.34\r\nlng: 56.78\r\nloc_id: 1\r\nreference_count: 0\r\nstatus: activated\r\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\n");
    }
}
