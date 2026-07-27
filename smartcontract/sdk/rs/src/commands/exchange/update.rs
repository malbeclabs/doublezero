use crate::DoubleZeroClient;
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::processors::exchange::update::ExchangeUpdateArgs;
use doublezero_serviceability_instruction::exchange::update_exchange;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateExchangeCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub name: Option<String>,
    pub lat: Option<f64>,
    pub lng: Option<f64>,
    pub bgp_community: Option<u16>,
}

impl UpdateExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let code = self
            .code
            .as_ref()
            .map(|code| validate_account_code(code))
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        client.send_transaction(update_exchange(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            ExchangeUpdateArgs {
                code,
                name: self.name.to_owned(),
                lat: self.lat,
                lng: self.lng,
                bgp_community: self.bgp_community,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::exchange::update::UpdateExchangeCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_exchange_pda, processors::exchange::update::ExchangeUpdateArgs,
    };
    use doublezero_serviceability_instruction::exchange::update_exchange;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_exchange_update_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_exchange_pda(&program_id, 1);

        let expected = update_exchange(
            &program_id,
            &payer,
            &pda_pubkey,
            ExchangeUpdateArgs {
                code: Some("test_exchange".to_string()),
                name: Some("Test Exchange".to_string()),
                lat: Some(0.0),
                lng: Some(0.0),
                bgp_community: Some(0),
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let update_command = UpdateExchangeCommand {
            pubkey: pda_pubkey,
            code: Some("test_exchange".to_string()),
            name: Some("Test Exchange".to_string()),
            lat: Some(0.0),
            lng: Some(0.0),
            bgp_community: Some(0),
        };

        let update_invalid_command = UpdateExchangeCommand {
            code: Some("test/exchange".to_string()),
            ..update_command.clone()
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());
        let res = update_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
