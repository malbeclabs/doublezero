use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::location::{get::GetLocationCommand, update::UpdateLocationCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateLocationCliCommand {
    /// Location Pubkey to update
    #[arg(long)]
    pub pubkey: String,
    /// Updated code for the location
    #[arg(long)]
    pub code: Option<String>,
    /// Updated name for the location
    #[arg(long)]
    pub name: Option<String>,
    /// Updated country for the location
    #[arg(long)]
    pub country: Option<String>,
    /// Updated latitude for the location
    #[arg(long, allow_hyphen_values(true))]
    pub lat: Option<f64>,
    /// Updated longitude for the location
    #[arg(long, allow_hyphen_values(true))]
    pub lng: Option<f64>,
    /// Updated location ID
    #[arg(long)]
    pub loc_id: Option<u32>,
}

impl UpdateLocationCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, location) = client.get_location(GetLocationCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let signature = client.update_location(UpdateLocationCommand {
            index: location.index,
            code: self.code,
            name: self.name,
            country: self.country,
            lat: self.lat,
            lng: self.lng,
            loc_id: self.loc_id,
        })?;

        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        location::update::UpdateLocationCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::location::update::UpdateLocationCommand, get_location_pda, AccountType,
        GetLocationCommand, Location, LocationStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_location_update() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_location_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let location = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "Test Country".to_string(),
            lat: 12.34,
            lng: 56.78,
            loc_id: 1,
            status: LocationStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_location()
            .with(predicate::eq(GetLocationCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, location.clone())));

        client
            .expect_update_location()
            .with(predicate::eq(UpdateLocationCommand {
                index: 1,
                code: Some("test".to_string()),
                name: Some("Test Location".to_string()),
                country: Some("Test Country".to_string()),
                lat: Some(12.34),
                lng: Some(56.78),
                loc_id: Some(1),
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        // Expected success
        let mut output = Vec::new();
        let res = UpdateLocationCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("test".to_string()),
            name: Some("Test Location".to_string()),
            country: Some("Test Country".to_string()),
            lat: Some(12.34),
            lng: Some(56.78),
            loc_id: Some(1),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
