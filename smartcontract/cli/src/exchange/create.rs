use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::exchange::create::CreateExchangeCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct CreateExchangeCliCommand {
    #[arg(long)]
    pub code: String,
    #[arg(long)]
    pub name: String,
    #[arg(long, allow_hyphen_values(true))]
    pub lat: f64,
    #[arg(long, allow_hyphen_values(true))]
    pub lng: f64,
    #[arg(long)]
    pub loc_id: Option<u32>,
}

impl CreateExchangeCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (signature, _pubkey) = client.create_exchange(CreateExchangeCommand {
            code: self.code.clone(),
            name: self.name.clone(),
            lat: self.lat,
            lng: self.lng,
            loc_id: self.loc_id,
        })?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        exchange::create::CreateExchangeCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{commands::exchange::create::CreateExchangeCommand, get_exchange_pda};
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_exchange_create() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_exchange_pda(&client.get_program_id(), 1);
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
            .expect_create_exchange()
            .with(predicate::eq(CreateExchangeCommand {
                code: "test".to_string(),
                name: "Test Exchange".to_string(),
                lat: 0.0,
                lng: 0.0,
                loc_id: None,
            }))
            .returning(move |_| Ok((signature, pda_pubkey)));

        let mut output = Vec::new();
        let res = CreateExchangeCliCommand {
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
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
