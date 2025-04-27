use clap::Args;
use doublezero_sdk::commands::exchange::create::CreateExchangeCommand;
use doublezero_sdk::*;

use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};

#[derive(Args, Debug)]
pub struct CreateExchangeArgs {
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

impl CreateExchangeArgs {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (signature, _pubkey) = CreateExchangeCommand {
            code: self.code.clone(),
            name: self.name.clone(),
            lat: self.lat,
            lng: self.lng,
            loc_id: self.loc_id,
        }
        .execute(client)?;
        println!("Signature: {}", signature);

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use doublezero_sdk::DoubleZeroClient;
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_exchange_pda},
        processors::exchange::create::ExchangeCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature, system_program};

    use crate::{exchange::create::CreateExchangeArgs, tests::tests::create_test_client};

    #[test]
    fn test_commands_exchange_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_exchange_pda(&client.get_program_id(), 1);
        let payer = client.get_payer();

        client.expect_get_balance().returning(|| Ok(150_000_000));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
                    index: 1,
                    code: "test".to_string(),
                    name: "Test Location".to_string(),
                    lat: 0.0,
                    lng: 0.0,
                    loc_id: 0,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(payer, true),
                    AccountMeta::new(system_program::id(), false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateExchangeArgs {
            code: "test".to_string(),
            name: "Test Location".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
