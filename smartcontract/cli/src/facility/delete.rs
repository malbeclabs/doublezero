use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::facility::{delete::DeleteFacilityCommand, get::GetFacilityCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteFacilityCliCommand {
    /// Facility Pubkey or code to delete
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
}

impl DeleteFacilityCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, _) = client.get_facility(GetFacilityCommand {
            pubkey_or_code: self.pubkey,
        })?;
        let signature = client.delete_facility(DeleteFacilityCommand { pubkey })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        facility::delete::DeleteFacilityCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::facility::delete::DeleteFacilityCommand, get_facility_pda, AccountType, Facility,
        FacilityStatus, GetFacilityCommand,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_facility_delete() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_facility_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let facility = Facility {
            account_type: AccountType::Facility,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            name: "Test Facility".to_string(),
            country: "Test Country".to_string(),
            lat: 12.34,
            lng: 56.78,
            loc_id: 1,
            status: FacilityStatus::Activated,
            owner: Pubkey::default(),
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        let facility_cloned = facility.clone();
        client
            .expect_get_facility()
            .with(predicate::eq(GetFacilityCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, facility_cloned.clone())));

        client
            .expect_delete_facility()
            .with(predicate::eq(DeleteFacilityCommand { pubkey: pda_pubkey }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = DeleteFacilityCliCommand {
            pubkey: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
