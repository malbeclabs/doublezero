use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    pda::get_exchange_pda, processors::exchange::create::ExchangeCreateArgs,
};
use doublezero_serviceability_instruction::exchange::create_exchange;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateExchangeCommand {
    pub code: String,
    pub name: String,
    pub lat: f64,
    pub lng: f64,
    pub bgp_community: Option<u16>,
}

impl CreateExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (_, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let program_id = client.get_program_id();
        let account_index = globalstate.account_index + 1;
        let (pda_pubkey, _) = get_exchange_pda(&program_id, account_index);

        let ix = create_exchange(
            &program_id,
            &client.get_payer(),
            account_index,
            ExchangeCreateArgs {
                code,
                name: self.name.clone(),
                lat: self.lat,
                lng: self.lng,
                reserved: 0, // BGP community is auto-assigned
            },
        );

        client.send_transaction(ix).map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::exchange::create::CreateExchangeCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::exchange::create::ExchangeCreateArgs;
    use doublezero_serviceability_instruction::exchange::create_exchange;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_exchange_create_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let expected = create_exchange(
            &program_id,
            &payer,
            1,
            ExchangeCreateArgs {
                code: "test_exchange".to_string(),
                name: "Test Exchange".to_string(),
                lat: 0.0,
                lng: 0.0,
                reserved: 0,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let create_command = CreateExchangeCommand {
            code: "test_exchange".to_string(),
            name: "Test Exchange".to_string(),
            lat: 0.0,
            lng: 0.0,
            bgp_community: None,
        };

        let create_invalid_command = CreateExchangeCommand {
            code: "test/command".to_string(),
            ..create_command.clone()
        };

        let res = create_command.execute(&client);
        assert!(res.is_ok());

        let res = create_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
