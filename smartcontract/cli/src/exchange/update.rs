use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
use doublezero_sdk::commands::exchange::update::UpdateExchangeCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateExchangeCliCommand {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub code: Option<String>,
    #[arg(long)]
    pub name: Option<String>,
    #[arg(long, allow_hyphen_values(true))]
    pub lat: Option<f64>,
    #[arg(long, allow_hyphen_values(true))]
    pub lng: Option<f64>,
    #[arg(long)]
    pub loc_id: Option<u32>,
}

impl UpdateExchangeCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, exchange) = GetExchangeCommand {
            pubkey_or_code: self.pubkey,
        }
        .execute(client)?;
        let signature = UpdateExchangeCommand {
            index: exchange.index,
            code: self.code,
            name: self.name,
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
    use crate::exchange::update::UpdateExchangeCliCommand;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::get_exchange_pda;
    use doublezero_sdk::AccountData;
    use doublezero_sdk::AccountType;
    use doublezero_sdk::DoubleZeroClient;
    use doublezero_sdk::Exchange;
    use doublezero_sdk::ExchangeStatus;
    use doublezero_sla_program::instructions::DoubleZeroInstruction;
    use doublezero_sla_program::pda::get_globalstate_pda;
    use doublezero_sla_program::processors::exchange::update::ExchangeUpdateArgs;
    use mockall::predicate;
    use solana_sdk::instruction::AccountMeta;
    use solana_sdk::pubkey::Pubkey;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_exchange_update() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_exchange_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 12.34,
            lng: 56.78,
            loc_id: 1,
            status: ExchangeStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
                    index: 1,
                    bump_seed,
                    code: Some("test".to_string()),
                    name: Some("Test Exchange".to_string()),
                    lat: Some(12.34),
                    lng: Some(56.78),
                    loc_id: Some(1),
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(move |_, _| Ok(signature));

        // Expected success
        let mut output = Vec::new();
        let res = UpdateExchangeCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("test".to_string()),
            name: Some("Test Exchange".to_string()),
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
