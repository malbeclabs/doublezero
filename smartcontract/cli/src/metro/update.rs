use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_code, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_sdk::{
    commands::metro::{get::GetMetroCommand, update::UpdateMetroCommand},
    BGP_COMMUNITY_MAX, BGP_COMMUNITY_MIN,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateMetroCliCommand {
    /// Metro Pubkey to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Updated code for the metro
    #[arg(long, value_parser = validate_code)]
    pub code: Option<String>,
    /// Updated name for the metro
    #[arg(long)]
    pub name: Option<String>,
    /// Updated latitude for the metro
    #[arg(long, allow_hyphen_values(true))]
    pub lat: Option<f64>,
    /// Updated longitude for the metro
    #[arg(long, allow_hyphen_values(true))]
    pub lng: Option<f64>,
    /// Re-assign BGP community value
    #[arg(long)]
    pub bgp_community: Option<u16>,
}

impl UpdateMetroCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        if let Some(bgp_community) = self.bgp_community {
            if !(BGP_COMMUNITY_MIN..=BGP_COMMUNITY_MAX).contains(&bgp_community) {
                return Err(eyre::eyre!(
                    "BGP community {} is out of valid range {}-{}",
                    bgp_community,
                    BGP_COMMUNITY_MIN,
                    BGP_COMMUNITY_MAX
                ));
            }
        }

        let (pubkey, _) = client.get_metro(GetMetroCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let signature = client.update_metro(UpdateMetroCommand {
            pubkey,
            code: self.code,
            name: self.name,
            lat: self.lat,
            lng: self.lng,
            bgp_community: self.bgp_community,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        metro::update::UpdateMetroCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::metro::{get::GetMetroCommand, update::UpdateMetroCommand},
        get_metro_pda, AccountType, Metro, MetroStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_exchange_update() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_metro_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let metro = Metro {
            account_type: AccountType::Metro,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            name: "Test Metro".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 12.34,
            lng: 56.78,
            bgp_community: 1,
            unused: 0,
            status: MetroStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_metro()
            .with(predicate::eq(GetMetroCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, metro.clone())));

        client
            .expect_update_metro()
            .with(predicate::eq(UpdateMetroCommand {
                pubkey: pda_pubkey,
                code: Some("test".to_string()),
                name: Some("Test Metro".to_string()),
                lat: Some(12.34),
                lng: Some(56.78),
                bgp_community: None,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        // Expected success
        let mut output = Vec::new();
        let res = UpdateMetroCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("test".to_string()),
            name: Some("Test Metro".to_string()),
            lat: Some(12.34),
            lng: Some(56.78),
            bgp_community: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_exchange_update_invalid_bgp_community() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_metro_pda(&client.get_program_id(), 1);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        // Test with BGP community below minimum
        let mut output = Vec::new();
        let res = UpdateMetroCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: None,
            name: None,
            lat: None,
            lng: None,
            bgp_community: Some(9999), // Below BGP_COMMUNITY_MIN (10000)
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert!(res.unwrap_err().to_string().contains("out of valid range"));

        // Test with BGP community above maximum
        let mut output = Vec::new();
        let res = UpdateMetroCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: None,
            name: None,
            lat: None,
            lng: None,
            bgp_community: Some(11000), // Above BGP_COMMUNITY_MAX (10999)
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert!(res.unwrap_err().to_string().contains("out of valid range"));
    }
}
