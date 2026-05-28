use crate::{doublezerocommand::CliCommand, validators::validate_code};
use clap::Args;
use doublezero_cli_core::{print_signature, require, CliContext, RequirementCheck};
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateLocationCliCommand {
    /// Unique location code
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Location name
    #[arg(long)]
    pub name: String,
    /// Country of the location
    #[arg(long)]
    pub country: String,
    /// Latitude of the location
    #[arg(long, allow_hyphen_values(true))]
    pub lat: f64,
    /// Longitude of the location
    #[arg(long, allow_hyphen_values(true))]
    pub lng: f64,
    #[arg(long)]
    pub loc_id: Option<u32>,
}

impl CreateLocationCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        require!(
            client,
            RequirementCheck::KEYPAIR | RequirementCheck::BALANCE
        );

        let (signature, _pubkey) = client.create_location(CreateLocationCommand {
            code: self.code,
            name: self.name,
            country: self.country,
            lat: self.lat,
            lng: self.lng,
            loc_id: self.loc_id,
        })?;

        print_signature(out, &signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        location::create::CreateLocationCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::cli_context_default_for_tests;
    use doublezero_sdk::{get_location_pda, CreateLocationCommand};
    use mockall::predicate;
    use solana_sdk::signature::Signature;
    use tokio::runtime::Builder;

    fn block_on<F: std::future::Future>(f: F) -> F::Output {
        Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(f)
    }

    #[test]
    fn test_cli_location_create() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_location_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_create_location()
            .with(predicate::eq(CreateLocationCommand {
                code: "test".to_string(),
                name: "Test Location".to_string(),
                country: "Test Country".to_string(),
                lat: 0.0,
                lng: 0.0,
                loc_id: None,
            }))
            .times(1)
            .returning(move |_| Ok((signature, pda_pubkey)));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            CreateLocationCliCommand {
                code: "test".to_string(),
                name: "Test Location".to_string(),
                country: "Test Country".to_string(),
                lat: 0.0,
                lng: 0.0,
                loc_id: None,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
