use crate::{commands::exchange::get::GetExchangeCommand, DoubleZeroClient};
use doublezero_serviceability::{
    processors::exchange::setdevice::{ExchangeSetDeviceArgs, SetDeviceOption},
    state::exchange::ExchangeStatus,
};
use doublezero_serviceability_instruction::exchange::set_device_exchange;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetDeviceExchangeCommand {
    pub pubkey: Pubkey,
    // Device pubkey for the exchange (to be set or unset)
    pub device1_pubkey: Option<Pubkey>,
    pub device2_pubkey: Option<Pubkey>,
}

impl SetDeviceExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (_, exchange) = GetExchangeCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)?;

        if exchange.status != ExchangeStatus::Activated {
            return Err(eyre::eyre!("Exchange is not active"));
        }

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let mut signature: Signature = Signature::default();

        if (exchange.device1_pk == Pubkey::default() && self.device1_pubkey.is_some())
            || (exchange.device1_pk != Pubkey::default() && self.device1_pubkey.is_none())
        {
            signature = client.send_transaction(set_device_exchange(
                &program_id,
                &payer,
                &self.pubkey,
                &self.device1_pubkey.unwrap_or(exchange.device1_pk),
                ExchangeSetDeviceArgs {
                    index: 1,
                    set: if self.device1_pubkey.is_some() {
                        SetDeviceOption::Set
                    } else {
                        SetDeviceOption::Remove
                    },
                },
            ))?;
        }

        if (exchange.device2_pk == Pubkey::default() && self.device2_pubkey.is_some())
            || (exchange.device2_pk != Pubkey::default() && self.device2_pubkey.is_none())
        {
            signature = client.send_transaction(set_device_exchange(
                &program_id,
                &payer,
                &self.pubkey,
                &self.device2_pubkey.unwrap_or(exchange.device2_pk),
                ExchangeSetDeviceArgs {
                    index: 2,
                    set: if self.device2_pubkey.is_some() {
                        SetDeviceOption::Set
                    } else {
                        SetDeviceOption::Remove
                    },
                },
            ))?;
        }

        Ok(signature)
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::exchange::setdevice::SetDeviceExchangeCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_exchange_pda,
        processors::exchange::setdevice::{ExchangeSetDeviceArgs, SetDeviceOption},
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            exchange::{Exchange, ExchangeStatus},
        },
    };
    use doublezero_serviceability_instruction::exchange::set_device_exchange;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_exchange_setdevice1_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (exchange_pubkey, _) = get_exchange_pda(&program_id, 1);
        let device_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            bgp_community: 1234,
            unused: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            status: ExchangeStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(exchange_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        let expected = set_device_exchange(
            &program_id,
            &payer,
            &exchange_pubkey,
            &device_pubkey,
            ExchangeSetDeviceArgs {
                index: 1,
                set: SetDeviceOption::Set,
            },
        );
        client
            .expect_send_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetDeviceExchangeCommand {
            pubkey: exchange_pubkey,
            device1_pubkey: Some(device_pubkey),
            device2_pubkey: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_exchange_setdevice2_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (exchange_pubkey, _) = get_exchange_pda(&program_id, 1);
        let device_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            bgp_community: 1234,
            unused: 0,
            device1_pk: device_pubkey,
            device2_pk: Pubkey::default(),
            status: ExchangeStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(exchange_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        let expected = set_device_exchange(
            &program_id,
            &payer,
            &exchange_pubkey,
            &device_pubkey,
            ExchangeSetDeviceArgs {
                index: 1,
                set: SetDeviceOption::Remove,
            },
        );
        client
            .expect_send_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetDeviceExchangeCommand {
            pubkey: exchange_pubkey,
            device1_pubkey: None,
            device2_pubkey: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_exchange_setdevice3_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (exchange_pubkey, _) = get_exchange_pda(&program_id, 1);

        let device1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let device2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzdvYqNpaRAUo");

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            bgp_community: 1234,
            unused: 0,
            device1_pk: device1_pubkey,
            device2_pk: device2_pubkey,
            status: ExchangeStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(exchange_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        let expected1 = set_device_exchange(
            &program_id,
            &payer,
            &exchange_pubkey,
            &device1_pubkey,
            ExchangeSetDeviceArgs {
                index: 1,
                set: SetDeviceOption::Remove,
            },
        );
        client
            .expect_send_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(expected1))
            .returning(|_| Ok(Signature::new_unique()));

        let expected2 = set_device_exchange(
            &program_id,
            &payer,
            &exchange_pubkey,
            &device2_pubkey,
            ExchangeSetDeviceArgs {
                index: 2,
                set: SetDeviceOption::Remove,
            },
        );
        client
            .expect_send_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(expected2))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetDeviceExchangeCommand {
            pubkey: exchange_pubkey,
            device1_pubkey: None,
            device2_pubkey: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_exchange_setdevice4_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (exchange_pubkey, _) = get_exchange_pda(&program_id, 1);

        let device1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let device2_pubkey = Pubkey::new_unique();

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::new_unique(),
            index: 1,
            bump_seed: 42,
            reference_count: 0,
            lat: 50.0,
            lng: 20.0,
            bgp_community: 1234,
            unused: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            status: ExchangeStatus::Activated,
            code: "TestExchange".to_string(),
            name: "TestName".to_string(),
        };

        let mut seq = mockall::Sequence::new();
        client
            .expect_get()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(exchange_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        let expected1 = set_device_exchange(
            &program_id,
            &payer,
            &exchange_pubkey,
            &device1_pubkey,
            ExchangeSetDeviceArgs {
                index: 1,
                set: SetDeviceOption::Set,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected1))
            .returning(|_| Ok(Signature::new_unique()));

        let expected2 = set_device_exchange(
            &program_id,
            &payer,
            &exchange_pubkey,
            &device2_pubkey,
            ExchangeSetDeviceArgs {
                index: 2,
                set: SetDeviceOption::Set,
            },
        );
        client
            .expect_send_transaction()
            .times(1)
            .in_sequence(&mut seq)
            .with(predicate::eq(expected2))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetDeviceExchangeCommand {
            pubkey: exchange_pubkey,
            device1_pubkey: Some(device1_pubkey),
            device2_pubkey: Some(device2_pubkey),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
