use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateLocationCliCommand {
    #[arg(long)]
    pub code: String,
    #[arg(long)]
    pub name: String,
    #[arg(long)]
    pub country: String,
    #[arg(long, allow_hyphen_values(true))]
    pub lat: f64,
    #[arg(long, allow_hyphen_values(true))]
    pub lng: f64,
    #[arg(long)]
    pub loc_id: Option<u32>,
}

impl CreateLocationCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (signature, _pubkey) = CreateLocationCommand {
            code: self.code.clone(),
            name: self.name.clone(),
            country: self.country.clone(),
            lat: self.lat,
            lng: self.lng,
            loc_id: self.loc_id,
        }
        .execute(client)?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use doublezero_sdk::get_location_pda;
    use doublezero_sdk::DoubleZeroClient;
    use doublezero_sla_program::instructions::DoubleZeroInstruction;
    use doublezero_sla_program::pda::get_globalstate_pda;
    use doublezero_sla_program::processors::location::create::LocationCreateArgs;
    use mockall::predicate;
    use solana_sdk::instruction::AccountMeta;
    use solana_sdk::signature::Signature;

    use crate::location::create::CreateLocationCliCommand;
    use crate::tests::tests::create_test_client;

    #[test]
    fn test_cli_location_create() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_location_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
                    index: 1,
                    bump_seed,
                    code: "test".to_string(),
                    name: "Test Location".to_string(),
                    country: "Test Country".to_string(),
                    lat: 0.0,
                    lng: 0.0,
                    loc_id: 0,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(move |_, _| Ok(signature));

        let mut output = Vec::new();
        let res = CreateLocationCliCommand {
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "Test Country".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
